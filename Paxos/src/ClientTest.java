import java.net.InetAddress;
import java.rmi.registry.LocateRegistry;
import java.rmi.registry.Registry;
import java.text.SimpleDateFormat;
import java.util.Date;
import java.util.ArrayList;
import java.util.List;
import java.rmi.RemoteException;
import java.util.Random;
import java.util.UUID;

/**
 * A test client that performs 5 PUTs, 5 GETs, and 5 DELETEEs automatically.
 * It verifies connectivity to a list of servers (host:port) and then selects one available server.
 */
public class ClientTest {

    /**
     * Entry point for the client test.
     * Usage: java ClientTest <serverAddressList>
     * where <serverAddressList> is a comma-separated list of addresses (e.g.,
     * "localhost:1099,localhost:1100,localhost:1101,localhost:1102,localhost:1103").
     */
    public static void main(String[] args) {
        // Preconfigured list of addresses.
        String serverListArg = "localhost:1099,localhost:1100,localhost:1101,localhost:1102,localhost:1103";

        // Parse the list into ServerAddress objects.
        String[] serverEntries = serverListArg.split(",");
        List<ServerAddress> availableServers = new ArrayList<>();
        for (String entry : serverEntries) {
            entry = entry.trim();
            String[] parts = entry.split(":");
            if (parts.length != 2) {
                System.err.println(getTimestamp() + " | Invalid server address format: " + entry);
                continue;
            }
            String host = parts[0].trim();
            int port = Integer.parseInt(parts[1].trim());

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

            // 5 PUTs.
            for (int i = 0; i < 5; i++) {
                String key = "key" + i;
                String value = "value" + i;
                String request = "put," + key + "," + value + "," + clientId;
                System.out.println(getTimestamp() + " | Sending PUT request: " + request);
                String response = remote.processRequest(request);
                System.out.println(getTimestamp() + " | PUT response: " + response);
                // Delay to allow for simulated failures and recoveries.
                Thread.sleep(3000);
            }

            // 5 GETs.
            for (int i = 0; i < 5; i++) {
                String key = "key" + i;
                String request = "get," + key + ",," + clientId;
                System.out.println(getTimestamp() + " | Sending GET request: " + request);
                String response = remote.processRequest(request);
                System.out.println(getTimestamp() + " | GET response: " + response);
                Thread.sleep(3000);
            }

            // 5 DELETEEs.
            for (int i = 0; i < 5; i++) {
                String key = "key" + i;
                String request = "delete," + key + ",," + clientId;
                System.out.println(getTimestamp() + " | Sending DELETE request: " + request);
                String response = remote.processRequest(request);
                System.out.println(getTimestamp() + " | DELETE response: " + response);
                Thread.sleep(3000);
            }

            System.out.println(getTimestamp() + " | Test operations completed.");

            // At the end, all 5 stores should be the same as the preconfigured originals.
            for (ServerAddress s : availableServers) {
                Registry curReg = LocateRegistry.getRegistry(s.host, s.port);
                KeyValueStoreRemote curRem = (KeyValueStoreRemote) curReg.lookup("KeyValueStore");
                curRem.showStore();
            }
        } catch (RemoteException e) {
            System.err.println(getTimestamp() + " | Remote error: " + e.getMessage());
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
