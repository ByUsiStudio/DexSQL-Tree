package org.dexsqltree.client;

import java.time.Instant;
import java.util.List;

/**
 * State of the engine's catalog cache, as reported by {@code /api/cache}.
 *
 * @param dir     directory holding the catalogs
 * @param enabled whether new datasets are cached
 * @param count   number of cached catalogs
 * @param bytes   total size on disk
 * @param entries one entry per cached catalog
 */
public record CacheInfo(String dir, boolean enabled, int count, long bytes, List<Entry> entries) {

    /** One cached catalog file. */
    public record Entry(String key, String path, long size, Instant modTime) {
    }
}
