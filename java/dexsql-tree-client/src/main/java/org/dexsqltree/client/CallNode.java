package org.dexsqltree.client;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;
import com.fasterxml.jackson.annotation.JsonProperty;

import java.util.List;

/** One node of a caller/callee tree. */
@JsonIgnoreProperties(ignoreUnknown = true)
public record CallNode(
        String descriptor,
        @JsonProperty("class") String className,
        String name,
        int fileId,
        int methodIdx,
        int depth,
        int calls,
        boolean cycle,
        List<CallNode> children) {

    public String shortName() {
        return name == null || name.isEmpty() ? descriptor : name;
    }
}
