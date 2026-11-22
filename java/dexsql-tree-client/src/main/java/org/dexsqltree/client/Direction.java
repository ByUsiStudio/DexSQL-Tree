package org.dexsqltree.client;

/** Traversal direction for call trees. */
public enum Direction {
    CALLERS("callers"),
    CALLEES("callees");

    private final String wire;

    Direction(String wire) {
        this.wire = wire;
    }

    public String wire() {
        return wire;
    }

    @Override
    public String toString() {
        return this == CALLERS ? "Callers" : "Callees";
    }
}
