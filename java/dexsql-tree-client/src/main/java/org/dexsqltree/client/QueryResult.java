package org.dexsqltree.client;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;

import java.util.List;

/** A read-only query result; values are String, Long, Double or null. */
@JsonIgnoreProperties(ignoreUnknown = true)
public record QueryResult(
        List<String> columns,
        List<List<Object>> rows,
        int rowCount,
        boolean truncated,
        long elapsedMs) {

    public Object cell(int row, int column) {
        List<Object> r = rows.get(row);
        return column < r.size() ? r.get(column) : null;
    }
}
