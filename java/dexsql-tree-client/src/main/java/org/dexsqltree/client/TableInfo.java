package org.dexsqltree.client;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;

import java.util.List;

/** Catalog entry: a table or relation exposed to SQL. */
@JsonIgnoreProperties(ignoreUnknown = true)
public record TableInfo(String name, List<String> cols, String doc) {
}
