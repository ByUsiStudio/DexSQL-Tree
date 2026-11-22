package org.dexsqltree.client;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;

import java.util.List;

/** Aggregate counters reported by the engine after parsing an input. */
@JsonIgnoreProperties(ignoreUnknown = true)
public record Stats(
        int files,
        long dexBytes,
        int strings,
        int types,
        int methods,
        int classes,
        int fields,
        int codeItems,
        List<String> warnings) {
}
