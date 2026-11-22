package org.dexsqltree.client;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;

/** Result of a call-tree build. */
@JsonIgnoreProperties(ignoreUnknown = true)
public record CallTree(
        CallNode root,
        String direction,
        int maxDepth,
        int nodeCount,
        int edgeCount,
        boolean truncated) {
}
