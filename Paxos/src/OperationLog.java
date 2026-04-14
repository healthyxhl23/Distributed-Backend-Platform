import java.io.Serializable;
import java.util.ArrayList;
import java.util.List;

/**
 * Simple class that logs a series of operations for failed node recovery
 * It provides methods to append operations and retrieve the log
 */
public class OperationLog implements Serializable {
    private final List<Operation> log = new ArrayList<>();

    public synchronized void append(Operation op) {
        log.add(op);
    }

    /**
     * Returns a copy of the log starting from the specified index.
     * If lastKnownIndex is 0, it returns all operations.
     */
    public synchronized List<Operation> getOperationsSince(int lastKnownIndex) {
        if (lastKnownIndex < 0 || lastKnownIndex >= log.size()) {
            return new ArrayList<>();
        }
        return new ArrayList<>(log.subList(lastKnownIndex, log.size()));
    }

    public synchronized int getSize() {
        return log.size();
    }
}
