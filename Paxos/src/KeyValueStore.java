import java.util.*;

/**
 * The key-value store for the servers
 * Contains 3 basic operations: put, get, delete
 */
public class KeyValueStore {
    private static final HashMap<String, String> map = new HashMap<>();

    public synchronized String put(String key, String value){
        map.put(key, value);
        return "Success:\n Key: " + key + " -> Value: " + value;
    }

    public synchronized String get(String key){
        return map.getOrDefault(key, "Failed:\n Key: (" + key + ") Not Found");
    }

    public synchronized String delete(String key){
        if (map.containsKey(key)){
            map.remove(key);
            return "Key: (" + key + ") Deleted!";
        } else {
            return "Key: (" + key + ") Not Found!";
        }
    }

    public synchronized void printAll(){
        if (map.isEmpty()){
            System.out.println("Empty");
            return;
        }

        System.out.println("Here is the current store: ");
        for (Map.Entry<String, String> entry : map.entrySet()) {
            System.out.println("Key: " + entry.getKey() + ", Value: " + entry.getValue());
        }


    }

    @Override
    public String toString() {
        return map.toString();
    }
}
