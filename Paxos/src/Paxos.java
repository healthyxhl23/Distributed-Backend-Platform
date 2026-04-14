import java.text.SimpleDateFormat;
import java.util.Date;
import java.util.Random;

/**
 * Paxos class that supports the prepare, accept, and reject phases.
 * It also simulates random acceptor failures using a background thread.
 */
public class Paxos {
    private long lastPromised = 0;

    // Flag to indicate whether this acceptor is active.
    // When false, the acceptor simulates a failure.
    private volatile boolean isActive = true;
    private final Random random = new Random();

    /**
     * Constructor. Starts the failure simulation thread.
     */
    public Paxos() {
        startFailureSimulation();
    }

    /**
     * The prepare phase.
     * Returns false if the acceptor is down.
     */
    public synchronized boolean prepare(String request, long proposalNumber) {
        if (!isActive) {
            System.out.println(getTimestamp() + " | [Paxos] prepare invoked, but acceptor is down.");
            return false;
        }
        if (proposalNumber > lastPromised) {
            lastPromised = proposalNumber;
            return true;
        }
        return false;
    }

    /**
     * The accept phase.
     * If the acceptor is down, always returns false to simulate failure.
     */
    public synchronized boolean accept(String request, long proposalNumber) {
        if (!isActive) {
            System.out.println(getTimestamp() + " | [Paxos] accept invoked, but acceptor is down.");
            return false;
        }
        return proposalNumber >= lastPromised;
    }

    /**
     * Simply logs a rejection and does nothing.
     */
    public synchronized void reject(String request, long proposalNumber) {
        System.out.println(getTimestamp() + " | [Paxos] reject called for proposal " + proposalNumber);
    }

    /**
     * Generates a unique proposal number.
     */
    public long generateProposalNumber(String serverId) {
        String[] parts = serverId.split("-");
        return System.currentTimeMillis() * 1000 + Integer.parseInt(parts[1]);
    }

    /**
     * Starts a daemon thread that simulates random failure and recovery.
     * The acceptor is active for a random period and then "fails" for a random period.
     */
    private void startFailureSimulation() {
        Thread failureThread = new Thread(() -> {
            while (true) {
                try {
                    // Active period: between 5 and 15 seconds.
                    int activeTime = random.nextInt(10000) + 5000;
                    Thread.sleep(activeTime);
                    // Simulate failure.
                    isActive = false;
                    System.out.println(getTimestamp() + " | [Paxos] Acceptor FAILURE: now down.");
                    // Failure period: between 5 and 15 seconds.
                    int failureTime = random.nextInt(10000) + 5000;
                    Thread.sleep(failureTime);
                    isActive = true;
                    System.out.println(getTimestamp() + " | [Paxos] Acceptor RECOVERED: now active.");
                } catch (InterruptedException e) {
                    System.out.println(getTimestamp() + " | [Paxos] Failure simulation thread interrupted.");
                }
            }
        });
        failureThread.setDaemon(true);
        failureThread.start();
    }

    private String getTimestamp() {
        return new SimpleDateFormat("yyyy-MM-dd HH:mm:ss.SSS").format(new Date());
    }
}
