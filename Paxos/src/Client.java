import java.io.*;
import java.net.InetAddress;
import java.text.SimpleDateFormat;
import java.util.Date;
import java.util.LinkedList;
import java.util.Queue;
import java.rmi.registry.LocateRegistry;
import java.rmi.registry.Registry;
import java.rmi.RemoteException;
import java.util.UUID;
import java.util.Random;
import java.util.ArrayList;
import java.util.List;

/**
 * Helper class to store server address information.
 */
class ServerAddress {
    String host;
    int port;

    public ServerAddress(String host, int port) {
        this.host = host;
        this.port = port;
    }
}

/**
 * Class to wrap a request and track its retry attempts.
 */
class RequestWrapper {
    String request;
    int attempts;

    public RequestWrapper(String request) {
        this.request = request;
        this.attempts = 0;
    }
}

/**
 * Client Class
 * Connects to one of the available RMI servers after verifying connectivity,
 * reads user input for key-value operations (put, get, delete, exit),
 * and handles sending requests with retry logic.
 */
public class Client {
    private static final int TIMEOUT = 10000;
    // Maximum number of retries per request
    private static final int MAX_RETRIES = 5;

    /**
     * Entry point for the client.
     * Usage: java Client <serverAddressList>
     * Example: java Client localhost:1099,localhost:1100,localhost:1101,localhost:1102,localhost:1103
     */
    public static void main(String[] args) {
        if (args.length < 1) {
            System.out.println(getTimestamp() + " | Usage: java Client <serverAddressList>");
            System.out.println("Example: java Client localhost:1099,localhost:1100,localhost:1101,localhost:1102,localhost:1103");
            return;
        }

        // Get the list of server addresses from the first argument.
        String serverListArg = args[0];
        String[] serverEntries = serverListArg.split(",");
        List<ServerAddress> availableServers = new ArrayList<>();

        // Verify connectivity by pinging each server.
        for (String entry : serverEntries) {
            entry = entry.trim();
            String[] parts = entry.split(":");
            if (parts.length != 2) {
                System.err.println(getTimestamp() + " | Invalid server address format: " + entry);
                continue;
            }
            String host = parts[0].trim();
            int port;
            try {
                port = Integer.parseInt(parts[1].trim());
            } catch (NumberFormatException nfe) {
                System.err.println(getTimestamp() + " | Invalid port number in: " + entry);
                continue;
            }
            try {
                Registry registry = LocateRegistry.getRegistry(host, port);
                KeyValueStoreRemote remote = (KeyValueStoreRemote) registry.lookup("KeyValueStore");
                // Call ping() to verify connectivity.
                if (remote.ping()) {
                    availableServers.add(new ServerAddress(host, port));
                    System.out.println(getTimestamp() + " | Host " + host + " on port " + port + " is available.");
                }
            } catch (Exception e) {
                System.err.println(getTimestamp() + " | Host " + host + " on port " + port + " is unavailable: " + e.getMessage());
            }
        }

        if (availableServers.isEmpty()) {
            System.err.println(getTimestamp() + " | No available hosts. Exiting.");
            return;
        }

        // Randomly select an available server.
        int index = new Random().nextInt(availableServers.size());
        ServerAddress selected = availableServers.get(index);
        System.out.println(getTimestamp() + " | Selected host: " + selected.host + " on port " + selected.port);

        try {
            InetAddress serverIP = InetAddress.getByName(selected.host);
            System.out.println(getTimestamp() + " | Resolved " + selected.host + " to " + serverIP.getHostAddress());
            String clientId = UUID.randomUUID().toString();
            int start = clientId.length() - 5;
            clientId = clientId.substring(start);
            System.out.println("ClientID: " + clientId);

            // Lookup the remote object in the RMI registry on the selected host.
            Registry registry = LocateRegistry.getRegistry(selected.host, selected.port);
            KeyValueStoreRemote remote = (KeyValueStoreRemote) registry.lookup("KeyValueStore");
            System.out.println(getTimestamp() + " | Connected to RMI server at " + selected.host + " on port " + selected.port);

            BufferedReader input = new BufferedReader(new InputStreamReader(System.in));
            Queue<RequestWrapper> requestQueue = new LinkedList<>();

            while (true) {
                // Get user command.
                String command;
                do {
                    System.out.println(getTimestamp() + " | Please enter a valid command (e.g., put, get, delete, exit): ");
                    command = input.readLine().toLowerCase();
                } while (!command.equals("put") && !command.equals("get") && !command.equals("delete") && !command.equals("exit"));

                if (command.equals("exit")) {
                    System.out.println(getTimestamp() + " | Client exiting...");
                    break;
                }

                // Keep prompting if key is empty.
                System.out.println(getTimestamp() + " | Please enter a key: ");
                String key = input.readLine();
                while (key.isEmpty()) {
                    System.out.println("Empty key is not accepted. Please enter again!");
                    System.out.println(getTimestamp() + " | Please enter a key: ");
                    key = input.readLine();
                }

                // Keep prompting if value is empty (for put).
                String value = "";
                if (command.equals("put")) {
                    System.out.println(getTimestamp() + " | Please enter a value: ");
                    value = input.readLine();
                    while (value.isEmpty()) {
                        System.out.println("Empty value is not accepted. Please enter again!");
                        System.out.println(getTimestamp() + " | Please enter a value: ");
                        value = input.readLine();
                    }
                }

                // Construct the request string: "command,key,value,clientId".
                String message = command + "," + key + "," + value + "," + clientId;
                requestQueue.add(new RequestWrapper(message));

                // Try sending all requests in the queue.
                while (!requestQueue.isEmpty()) {
                    RequestWrapper wrapper = requestQueue.poll();
                    String request = wrapper.request;
                    System.out.println(getTimestamp() + " | Sending request: " + request);
                    try {
                        String response = remote.processRequest(request);
                        if (response.isEmpty()) {
                            System.err.println(getTimestamp() + " | Error: Received empty or malformed response from server.");
                        } else {
                            System.out.println(getTimestamp() + " | Received response from server.");
                            System.out.println("Response:\n" + response);
                        }
                    } catch (RemoteException e) {
                        System.err.println(getTimestamp() + " | Error: " + e.getMessage());
                        wrapper.attempts++;
                        if (wrapper.attempts < MAX_RETRIES) {
                            System.err.println(getTimestamp() + " | Re-queueing failed request (attempt " + wrapper.attempts + ").");
                            requestQueue.add(wrapper);
                        } else {
                            System.err.println(getTimestamp() + " | Maximum retries reached for request: " + request);
                        }
                    }
                }
            }
        } catch (Exception e) {
            System.err.println(getTimestamp() + " | Error: " + e.getMessage());
        }
    }

    /**
     * Returns the current system time with millisecond precision.
     *
     * @return A timestamp string (e.g., "2025-02-28 15:23:05.627")
     */
    private static String getTimestamp() {
        return new SimpleDateFormat("yyyy-MM-dd HH:mm:ss.SSS").format(new Date());
    }
}
