import java.io.Serializable;


/**
 * Simple class that represents an operation (put, get, delete) along with any metadata
 */
public class Operation implements Serializable {
    private final String command; // e.g., "put" or "delete"
    private final String key;
    private final String value;   // For delete, this could be null.
    private final long timestamp;

    public Operation(String command, String key, String value, long timestamp) {
        this.command = command;
        this.key = key;
        this.value = value;
        this.timestamp = timestamp;
    }

    public String getCommand() { return command; }
    public String getKey() { return key; }
    public String getValue() { return value; }
    public long getTimestamp() { return timestamp; }

    @Override
    public String toString() {
        return "Operation [command=" + command + ", key=" + key + ", value=" + value + ", timestamp=" + timestamp + "]";
    }
}
