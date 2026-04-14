import java.rmi.Remote;
import java.rmi.RemoteException;
import java.util.List;

/**
 * Remote interface for the Key-Value Store.
 * Methods for processing requests and operations for Paxos.
 */
public interface KeyValueStoreRemote extends Remote {

    /**
     * Processes a client request in the format "command,key,value,userID".
     * Supported commands are: put, get, delete, exit.
     *
     * @param request A string representing the operation to perform.
     * @return A response message from the server.
     * @throws RemoteException If an RMI communication error occurs.
     */
    String processRequest(String request) throws RemoteException;

    /**
     * Remote method for the Paxos prepare phase.
     * @param request The transaction details.
     * @return True if the participant is ready to commit; false otherwise.
     * @throws RemoteException If an RMI communication error occurs.
     */
    boolean prepare(String request, long proposalNumber) throws RemoteException;

    /**
     * Remote method for the Paxos accept phase.
     * The server attempts to accept the proposal with the given number and request.
     *
     * @param request The transaction details.
     * @param proposalNumber  The unique proposal number.
     * @return True if the proposal is accepted, false otherwise.
     * @throws RemoteException If an RMI communication error occurs.
     */
    boolean accept(String request, long proposalNumber) throws RemoteException;

    /**
     * Remote method to reject a proposal.
     *
     * @param request The transaction details.
     * @param proposalNumber  The unique proposal number.
     * @throws RemoteException If an RMI communication error occurs.
     */
    void reject(String request, long proposalNumber) throws RemoteException;

    /**
     * Remote method for the Paxos learn phase.
     * Once consensus is reached, this method is called to inform the server of the decided proposal so that it can commit
     * the change locally.
     *
     * @param request The transaction details.
     * @param proposalNumber  The unique proposal number.
     * @throws RemoteException If an RMI communication error occurs.
     */
    void learn(String request, long proposalNumber) throws RemoteException;

    /**
     * Used to verify that the server is running and accessible.
     *
     * @return True if the server is up and responding; false otherwise.
     * @throws RemoteException If an RMI communication error occurs.
     */
    boolean ping() throws RemoteException;

    /**
     * Shows the current key-value store.
     *
     * @throws RemoteException If an RMI communication error occurs.
     */
    void showStore() throws RemoteException;

    /**
     * Shows the previous states of the missing operations
     * @param lastIndex index of the last operation performed
     * @return A list of operations since the log at lastIndex
     * @throws RemoteException
     */

    List<Operation> getOperationsSince(int lastIndex) throws RemoteException;
}
