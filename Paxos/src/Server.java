import com.sun.net.httpserver.HttpServer;
import java.io.IOException;
import java.io.OutputStream;
import java.net.InetSocketAddress;
import java.net.URLDecoder;
import java.nio.charset.StandardCharsets;
import java.rmi.registry.LocateRegistry;
import java.rmi.registry.Registry;
import java.rmi.RemoteException;
import java.rmi.server.UnicastRemoteObject;
import java.text.SimpleDateFormat;
import java.util.Date;
import java.util.List;
import java.util.ArrayList;

/**
 * RMI Server class that implements a Paxos-based replicated key-value store.
 * Each server participates as a proposer, acceptor, and learner.
 * All Paxos operations are delegated to the Paxos component.
 * Additionally, the Bully algorithm is used to perform leader election among servers.
 */
public class Server extends UnicastRemoteObject implements KeyValueStoreRemote {
    // Local key-value store instance.
    private final KeyValueStore store = new KeyValueStore();
    // List of other replica servers.
    private final List<KeyValueStoreRemote> replicas = new ArrayList<>();
    // Unique identifier for this server.
    private final String serverId;

    // Common Paxos logic.
    private final Paxos paxos;

    // Leader election state.
    private volatile String leaderId = null;
    private volatile boolean isLeader = false;

    // Operation log for recovery if node failed.
    private final OperationLog opLog = new OperationLog();
    //The index of the last known operation.
    private int lastKnownIndex = 0;

    /**
     * Constructor: Initializes server state, pre-populates the KV store, and starts leader monitoring.
     *
     * @param serverId Unique identifier for this server.
     */
    protected Server(String serverId) throws RemoteException {
        super();
        this.serverId = serverId;
        this.paxos = new Paxos();
        // Pre-populate key-value store.
        store.put("a", "1");
        store.put("b", "2");
        store.put("c", "3");
        store.put("d", "4");
        store.put("e", "5");
        store.put("f", "6");
        store.put("g", "7");
        store.put("h", "8");

        // Pre-populate rate limit configs consumed by the Go config loader.
        // Values are JSON so the loader can unmarshal them directly.
        store.put("rate:config:default", "{\"limit\":100,\"window_ms\":60000,\"algorithm\":\"token_bucket\"}");
        store.put("rate:config:free",    "{\"limit\":50,\"window_ms\":60000,\"algorithm\":\"sliding_window\"}");
        store.put("rate:config:premium", "{\"limit\":1000,\"window_ms\":60000,\"algorithm\":\"token_bucket\"}");

        // Start the leader monitor thread.
        startLeaderMonitor();
    }

    @Override
    public void showStore() throws RemoteException {
        System.out.println("Server [" + serverId + "]: ");
        System.out.println(store);
        System.out.println("-------------------------------");
    }

    /**
     * Processes client requests.
     * GET operations are processed locally.
     * For PUT and DELETE operations, a Paxos consensus protocol is initiated.
     */
    @Override
    public synchronized String processRequest(String request) throws RemoteException {
        String timestamp = getTimestamp();
        String[] parts = request.split(",");
        if (parts.length != 4) {
            String errorMsg = timestamp + " | [" + serverId + "] Received malformed request from client";
            System.err.println(errorMsg);
            return "Error: Malformed request";
        }
        String command = parts[0];
        String key = parts[1];
        String value = parts[2];
        String clientID = parts[3];

        String response;
        if (command.equals("get")) {
            response = store.get(key);
        } else if (command.equals("put") || command.equals("delete")) {
            response = processPaxos(request);
        } else {
            response = timestamp + " | [" + serverId + "] Error: Unknown command";
        }

        System.out.println(timestamp + " | [" + serverId + "] Request: " + request);
        System.out.println(timestamp + " | [" + serverId + "] Response sent to client <" + clientID + ">: " + response);
        store.printAll();
        System.out.println("--------------------------------\n");

        return response;
    }

    /**
     * Implements update operations using a three-phase Paxos consensus with retry logic.
     */
    private String processPaxos(String request) throws RemoteException {
        String timestamp = getTimestamp();
        final int maxRetries = 8;
        final int baseDelay = 1000; // in milliseconds

        for (int attempt = 0; attempt < maxRetries; attempt++) {
            long proposalNumber = paxos.generateProposalNumber(serverId);

            // Phase 1: Prepare Phase
            int promiseCount = 0;
            for (KeyValueStoreRemote replica : replicas) {
                try {
                    if (replica.prepare(request, proposalNumber)) {
                        promiseCount++;
                    }
                } catch (RemoteException e) {
                    System.out.println(timestamp + " | replica down");
                }
            }
            if (this.prepare(request, proposalNumber)) {
                promiseCount++;
            }
            if (promiseCount < (replicas.size() + 1) / 2 + 1) {
                System.out.println(timestamp + " | [" + serverId + "] Attempt " + attempt + ": Insufficient promises (" + promiseCount + "). Retrying...");
                try {
                    Thread.sleep(baseDelay * (attempt + 1));
                } catch (InterruptedException ie) {
                    Thread.currentThread().interrupt();
                }
                continue;
            }

            // Phase 2: Accept Phase
            int acceptCount = 0;
            for (KeyValueStoreRemote replica : replicas) {
                try {
                    if (replica.accept(request, proposalNumber)) {
                        acceptCount++;
                    }
                } catch (RemoteException e) {
                    System.out.println(timestamp + " | Replica down");
                }
            }
            if (this.accept(request, proposalNumber)) {
                acceptCount++;
            }
            if (acceptCount < (replicas.size() + 1) / 2 + 1) {
                System.out.println(timestamp + " | [" + serverId + "] Attempt " + attempt + ": Insufficient accepts (" + acceptCount + "). Retrying...");
                for (KeyValueStoreRemote replica : replicas) {
                    replica.reject(request, proposalNumber);
                }
                try {
                    Thread.sleep(baseDelay * (attempt + 1));
                } catch (InterruptedException ie) {
                    Thread.currentThread().interrupt();
                }
                continue;
            }

            // Phase 3: Learn Phase
            for (KeyValueStoreRemote replica : replicas) {
                replica.learn(request, proposalNumber);
            }
            String result = processLocalCommit(request);
            // Update the last-known index for recovery.
            lastKnownIndex = opLog.getSize();
            return timestamp + " | " + result + " | [" + serverId + "] Success: Operation committed via Paxos on attempt " + attempt;
        }

        return timestamp + " | [" + serverId + "] Error: Operation failed after " + maxRetries + " retries";
    }

    /**
     * Applies an update locally on this server and logs the operation.
     */
    private String processLocalCommit(String request) {
        String[] parts = request.split(",");
        String command = parts[0];
        String key = parts[1];
        String value = parts[2];
        String timestamp = getTimestamp();

        // Create an operation for logging.
        Operation op = new Operation(command, key, value, System.currentTimeMillis());

        if (command.equals("put")) {
            String result = store.put(key, value);
            opLog.append(op);
            return result;
        } else if (command.equals("delete")) {
            String result = store.delete(key);
            opLog.append(op);
            if (result.toLowerCase().contains("not found")) {
                return "Key: (" + key + ") Deleted!";
            }
            return result;
        }
        return "Error: Unknown command in local update";
    }

    /**
     * Remote method for the Paxos prepare phase.
     * Delegates to the Paxos component.
     */
    @Override
    public synchronized boolean prepare(String request, long proposalNumber) throws RemoteException {
        return paxos.prepare(request, proposalNumber);
    }

    /**
     * Remote method for the Paxos accept phase.
     * Delegates to the Paxos component.
     */
    @Override
    public synchronized boolean accept(String request, long proposalNumber) throws RemoteException {
        return paxos.accept(request, proposalNumber);
    }

    /**
     * Remote method for the Paxos reject phase.
     * Delegates to the Paxos component.
     */
    @Override
    public synchronized void reject(String request, long proposalNumber) throws RemoteException {
        paxos.reject(request, proposalNumber);
    }

    /**
     * Remote method for the Paxos learn phase.
     * Applies the committed update locally.
     */
    @Override
    public synchronized void learn(String request, long proposalNumber) throws RemoteException {
        System.out.println(getTimestamp() + " | [" + serverId + "] learns proposal " + proposalNumber);
        processLocalCommit(request);
    }

    /**
     * Checks if current server is active.
     */
    @Override
    public boolean ping() throws RemoteException {
        return true;
    }

    /**
     * Remote method added to allow a recovering node to fetch operations it missed.
     */
    @Override
    public synchronized List<Operation> getOperationsSince(int lastIndex) throws RemoteException {
        return opLog.getOperationsSince(lastIndex);
    }

    // Bully Leader Election Methods

    /**
     * Initiates the Bully election algorithm.
     * Checks if any replica with a higher priority is alive.
     * If none are alive, this server declares itself as leader and announces it.
     */
    public synchronized void startElection() throws RemoteException {
        String timestamp = getTimestamp();
        System.out.println(timestamp + " | [" + serverId + "] Starting election");
        boolean higherAlive = false;
        for (KeyValueStoreRemote replica : replicas) {
            if (replica instanceof Server s) {
                if (s.getPriority() > this.getPriority()) {
                    try {
                        if (s.ping()) {
                            higherAlive = true;
                            System.out.println(timestamp + " | Current node: [" + serverId + "] | A higher priority node [" + s.serverId + "] is alive");
                        }
                    } catch (RemoteException e) {
                        System.out.println("No higher-priority node");
                    }
                }
            }
        }
        if (!higherAlive) {
            isLeader = true;
            leaderId = serverId;
            System.out.println(timestamp + " | [" + serverId + "] I am elected as leader");
            for (KeyValueStoreRemote replica : replicas) {
                if (replica instanceof Server) {
                    try {
                        ((Server) replica).announceLeader(serverId);
                    } catch (RemoteException e) {
                        // Ignore if announcement fails.
                    }
                }
            }
        } else {
            System.out.println(timestamp + " | [" + serverId + "] A higher priority node is alive; waiting for leader announcement");
        }
    }

    /**
     * Announces the leader to this server.
     */
    public synchronized void announceLeader(String newLeaderId) throws RemoteException {
        String timestamp = getTimestamp();
        leaderId = newLeaderId;
        isLeader = serverId.equals(newLeaderId);
        System.out.println(timestamp + " | [" + serverId + "] Leader announced: " + newLeaderId);
    }

    /**
     * Returns this server's priority by its serverId.
     * Assumes serverId is of the form "Server-<number>".
     *
     * @return The numeric priority.
     */
    private int getPriority() {
        String[] parts = serverId.split("-");
        try {
            return Integer.parseInt(parts[1]);
        } catch (Exception e) {
            return 0;
        }
    }

    /**
     * Periodically checks if the current leader is alive.
     * If the leader is dead, a new election occurs.
     */
    private void startLeaderMonitor() {
        new Thread(() -> {
            while (true) {
                try {
                    Thread.sleep(5000);
                    // If there is no leader or down, start new election
                    if (leaderId == null || (!isLeader && !isLeaderAlive())) {
                        startElection();
                        // If you are not the leader, recover the missing state if any
                        if (!isLeader) {
                            lastKnownIndex = recoverState(lastKnownIndex);
                        }
                    }
                } catch (InterruptedException | RemoteException ignored) {
                }
            }
        }).start();
    }

    /**
     * Checks if the current leader is alive by pinging it.
     *
     * @return True if the leader responds; false otherwise.
     */
    private boolean isLeaderAlive() {
        // Self is leader
        if (isLeader) {
            return true;
        }
        // Find leader
        for (KeyValueStoreRemote replica : replicas) {
            if (replica instanceof Server s) {
                if (s.serverId.equals(leaderId)) {
                    try {
                        return s.ping();
                    } catch (RemoteException e) {
                        return false;
                    }
                }
            }
        }
        return isLeader;
    }

    /**
     * Recovers the local state by fetching missing operations from the leader.
     * @param lastKnownIndex The index of the last operation this node has.
     */
    public Integer recoverState(int lastKnownIndex) {
        try {
            String timestamp = getTimestamp();
            if (leaderId == null || leaderId.equals(serverId)) {
                return lastKnownIndex;
            }
            for (KeyValueStoreRemote replica : replicas) {
                // Find leader to query for missed operations and apply them to self
                if (replica instanceof Server s) {
                    if (s.serverId.equals(leaderId)) {
                        List<Operation> missingOps = s.getOperationsSince(lastKnownIndex);
                        for (Operation op : missingOps) {
                            if (op.getCommand().equals("put")) {
                                store.put(op.getKey(), op.getValue());
                            } else if (op.getCommand().equals("delete")) {
                                store.delete(op.getKey());
                            }
                            opLog.append(op);
                        }
                        System.out.println(timestamp + " | [" + serverId + "] Recovered " + missingOps.size() + " operations from leader " + leaderId);
                        return opLog.getSize();
                    }
                }
            }
            System.out.println(getTimestamp() + " | [" + serverId + "] Leader not found in replica list for recovery.");
        } catch (Exception e) {
            System.err.println(getTimestamp() + " | [" + serverId + "] Error during recovery: " + e.getMessage());
        }
        return lastKnownIndex;
    }

    /**
     * Starts a lightweight HTTP bridge so external clients (e.g. the Go config
     * loader) can query the KV store without going through RMI.
     *
     * Routes:
     *   GET /health          → 200 OK  (liveness probe)
     *   GET /get?key=<key>   → 200 OK + JSON body, or 404 if key not found
     *
     * @param httpPort the port to listen on (typically RMI port + 1000)
     */
    void startHttpBridge(int httpPort) {
        try {
            HttpServer http = HttpServer.create(new InetSocketAddress(httpPort), /*backlog=*/0);

            // Liveness probe — Go Loader.Ping() calls this.
            http.createContext("/health", exchange -> {
                byte[] body = "OK".getBytes(StandardCharsets.UTF_8);
                exchange.sendResponseHeaders(200, body.length);
                try (OutputStream os = exchange.getResponseBody()) {
                    os.write(body);
                }
            });

            // Key lookup — Go Loader.fetchFromKV() calls GET /get?key=<key>.
            http.createContext("/get", exchange -> {
                String key = null;
                String query = exchange.getRequestURI().getQuery();
                if (query != null) {
                    for (String param : query.split("&")) {
                        String[] kv = param.split("=", 2);
                        if (kv.length == 2 && kv[0].equals("key")) {
                            key = URLDecoder.decode(kv[1], StandardCharsets.UTF_8);
                            break;
                        }
                    }
                }
                if (key == null || key.isBlank()) {
                    exchange.sendResponseHeaders(400, -1);
                    exchange.close();
                    return;
                }
                String value = store.get(key);
                if (value.startsWith("Failed:")) {
                    exchange.sendResponseHeaders(404, -1);
                    exchange.close();
                    return;
                }
                byte[] body = value.getBytes(StandardCharsets.UTF_8);
                exchange.getResponseHeaders().set("Content-Type", "application/json");
                exchange.sendResponseHeaders(200, body.length);
                try (OutputStream os = exchange.getResponseBody()) {
                    os.write(body);
                }
            });

            http.setExecutor(null); // use default single-threaded executor
            http.start();
            System.out.println(getTimestamp() + " | [" + serverId + "] HTTP bridge listening on port " + httpPort);
        } catch (IOException e) {
            System.err.println(getTimestamp() + " | [" + serverId + "] HTTP bridge failed to start: " + e.getMessage());
        }
    }

    // Main method to launch server.
    public static void main(String[] args) {
        int port = 1099;
        if (args.length >= 1) {
            port = Integer.parseInt(args[0]);
        } else {
            System.out.println("Usage: java Server <port>");
            System.out.println("Defaulting to port 1099.");
        }

        List<Server> servers = new ArrayList<>();

        try {
            for (int i = 0; i < 5; i++) {
                String id = "Server-" + (port + i);
                Server s = new Server(id);
                servers.add(s);
            }
            for (Server s : servers) {
                for (Server replica : servers) {
                    if (!replica.equals(s)) {
                        s.replicas.add(replica);
                    }
                }
            }
            for (int i = 0; i < servers.size(); i++) {
                int currentPort = port + i;
                Server s = servers.get(i);
                Registry registry = LocateRegistry.createRegistry(currentPort);
                registry.bind("KeyValueStore", s);
                s.startHttpBridge(currentPort + 1000);
                System.out.println(getTimestamp() + " | " + s.serverId + " listening on port " + currentPort);
                s.store.printAll();
                System.out.println("--------------------------------\n");
            }
        } catch (Exception e) {
            System.err.println(getTimestamp() + " | Error: " + e.getMessage());
        }
    }

    private static String getTimestamp() {
        return new SimpleDateFormat("yyyy-MM-dd HH:mm:ss.SSS").format(new Date());
    }
}
