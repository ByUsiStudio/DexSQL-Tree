package org.dexsqltree.client;

/**
 * Engine health and effective settings, as reported by {@code /api/health}.
 *
 * @param ok      server reachable and ready
 * @param version engine version
 * @param lang    prompt language ("en" or "zh")
 * @param profile materialisation profile in use ("fast" or "full")
 * @param cache   whether the persistent catalog cache is enabled
 */
public record Health(boolean ok, String version, String lang, String profile, boolean cache) {
}
