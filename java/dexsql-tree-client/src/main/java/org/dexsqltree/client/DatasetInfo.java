package org.dexsqltree.client;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;

import java.util.List;

/** One opened APK/DEX dataset. */
@JsonIgnoreProperties(ignoreUnknown = true)
public record DatasetInfo(String id, String path, Stats stats, List<String> warnings) {
}
