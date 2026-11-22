package org.dexsqltree.client;

/** Raised for transport failures and structured errors returned by the engine. */
public class DexsqlException extends RuntimeException {
    private final int status;

    public DexsqlException(String message) {
        this(message, 0, null);
    }

    public DexsqlException(String message, int status, Throwable cause) {
        super(message, cause);
        this.status = status;
    }

    public int status() {
        return status;
    }
}
