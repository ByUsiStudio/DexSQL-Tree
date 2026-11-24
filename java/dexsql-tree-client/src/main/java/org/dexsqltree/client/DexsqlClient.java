package org.dexsqltree.client;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ObjectNode;

import java.io.IOException;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.charset.StandardCharsets;
import java.nio.file.Path;
import java.time.Duration;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.ThreadFactory;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.function.Supplier;

/**
 * Client for the dexsql-tree HTTP API.
 *
 * <p>Every request can be issued either blocking or through {@link #async},
 * which runs on a shared daemon pool. The engine is single-writer per dataset
 * but readers are independent, so a small fixed pool (one thread per core, at
 * most eight) keeps the UI responsive while several queries, a tree and a
 * disassembly are in flight.
 *
 * <p>Methods that take an explicit {@code dataset} never touch shared state;
 * the {@code datasetId} field is only a convenience default for single-dataset
 * sessions. That keeps the class safe to use from several threads at once.
 */
public final class DexsqlClient {
    private static final int CORE_POOL = Math.max(2, Math.min(8, Runtime.getRuntime().availableProcessors()));

    private static final ExecutorService POOL = Executors.newFixedThreadPool(CORE_POOL, new ThreadFactory() {
        private final AtomicInteger seq = new AtomicInteger();

        @Override
        public Thread newThread(Runnable r) {
            Thread t = new Thread(r, "dexsql-http-" + seq.incrementAndGet());
            t.setDaemon(true);
            return t;
        }
    });

    private final URI base;
    private final HttpClient http;
    // findAndRegisterModules picks up jackson-datatype-jsr310, which is required
    // to read the Instant timestamps in CacheInfo.
    private final ObjectMapper mapper = new ObjectMapper().findAndRegisterModules();
    private final Duration requestTimeout;
    private final String token;
    private volatile String datasetId;

    /** Shared worker pool used by the async methods and by GUI tasks. */
    public static ExecutorService executor() {
        return POOL;
    }

    public static int poolSize() {
        return CORE_POOL;
    }

    public DexsqlClient(String baseUrl) {
        this(baseUrl, Duration.ofMinutes(5));
    }

    public DexsqlClient(String baseUrl, String token) {
        this(baseUrl, Duration.ofMinutes(5), token);
    }

    public DexsqlClient(String baseUrl, Duration requestTimeout) {
        this(baseUrl, requestTimeout, null);
    }

    /** A client that sends {@code Authorization: Bearer <token>} on every call. */
    public DexsqlClient(String baseUrl, Duration requestTimeout, String token) {
        String url = baseUrl.endsWith("/") ? baseUrl.substring(0, baseUrl.length() - 1) : baseUrl;
        this.base = URI.create(url);
        this.requestTimeout = requestTimeout;
        this.token = token == null || token.isBlank() ? null : token;
        this.http = HttpClient.newBuilder()
                .connectTimeout(Duration.ofSeconds(5))
                .followRedirects(HttpClient.Redirect.NORMAL)
                .executor(POOL)
                .build();
    }

    public String baseUrl() {
        return base.toString();
    }

    /** The dataset used when a call omits the dataset argument. */
    public String datasetId() {
        return datasetId;
    }

    public void useDataset(String id) {
        this.datasetId = id;
    }

    // ------------------------------------------------------------------ info

    public boolean health() {
        try {
            return get("/api/health").path("ok").asBoolean(false);
        } catch (RuntimeException e) {
            return false;
        }
    }

    public Health healthInfo() {
        return mapper.convertValue(get("/api/health"), Health.class);
    }

    public List<DatasetInfo> datasets() {
        JsonNode node = get("/api/datasets");
        List<DatasetInfo> out = new ArrayList<>();
        for (JsonNode d : node.path("datasets")) {
            out.add(mapper.convertValue(d, DatasetInfo.class));
        }
        return out;
    }

    public List<TableInfo> tables() {
        JsonNode node = get("/api/tables");
        List<TableInfo> out = new ArrayList<>();
        for (JsonNode t : node.path("tables")) {
            out.add(mapper.convertValue(t, TableInfo.class));
        }
        return out;
    }

    public DatasetInfo openDataset(Path path) {
        ObjectNode body = mapper.createObjectNode();
        body.put("path", path.toAbsolutePath().toString());
        DatasetInfo info = post("/api/datasets", body, DatasetInfo.class);
        if (info.id() != null) {
            datasetId = info.id();
        }
        return info;
    }

    // --------------------------------------------------------------- queries

    public QueryResult query(String sql, int limit) {
        return query(sql, limit, datasetId);
    }

    public QueryResult query(String sql) {
        return query(sql, 1000, datasetId);
    }

    public QueryResult query(String sql, int limit, String dataset) {
        ObjectNode body = mapper.createObjectNode();
        body.put("sql", sql);
        body.put("limit", limit);
        return post("/api/query", withDataset(body, dataset), QueryResult.class);
    }

    /** Runs a built-in recipe such as {@code callers} with its arguments. */
    public QueryResult recipe(String name, java.util.Map<String, String> args, int limit, String dataset) {
        ObjectNode body = mapper.createObjectNode();
        body.put("recipe", name);
        ObjectNode a = body.putObject("args");
        if (args != null) {
            args.forEach(a::put);
        }
        body.put("limit", limit);
        return post("/api/query", withDataset(body, dataset), QueryResult.class);
    }

    /** Keyword search across classes, methods, fields and strings. */
    public QueryResult search(String keyword, List<String> kinds, int limit, String dataset) {
        StringBuilder url = new StringBuilder("/api/search?keyword=").append(urlEncode(keyword));
        if (kinds != null) {
            for (String k : kinds) {
                url.append("&kind=").append(urlEncode(k));
            }
        }
        url.append("&limit=").append(limit);
        if (dataset != null && !dataset.isBlank()) {
            url.append("&dataset=").append(urlEncode(dataset));
        }
        return mapper.convertValue(get(url.toString()), QueryResult.class);
    }

    public CallTree tree(String method, Direction direction, int depth) {
        return tree(method, direction, depth, datasetId);
    }

    public CallTree tree(String method, Direction direction, int depth, String dataset) {
        ObjectNode body = mapper.createObjectNode();
        body.put("method", method);
        body.put("direction", direction.wire());
        body.put("depth", depth);
        return post("/api/tree", withDataset(body, dataset), CallTree.class);
    }

    public String smali(String method) {
        return smali(method, datasetId);
    }

    public String smali(String method, String dataset) {
        String url = "/api/smali?method=" + urlEncode(method)
                + (dataset == null || dataset.isBlank() ? "" : "&dataset=" + urlEncode(dataset));
        return get(url).path("smali").asText();
    }

    // ---------------------------------------------------------------- cache

    public CacheInfo cache() {
        return mapper.convertValue(get("/api/cache"), CacheInfo.class);
    }

    public CacheInfo clearCache() {
        HttpRequest req = request("/api/cache").DELETE().build();
        send(req);
        return cache();
    }

    // ---------------------------------------------------------------- async

    public CompletableFuture<QueryResult> queryAsync(String sql, int limit, String dataset) {
        return async(() -> query(sql, limit, dataset));
    }

    public CompletableFuture<QueryResult> searchAsync(String keyword, List<String> kinds, int limit, String dataset) {
        return async(() -> search(keyword, kinds, limit, dataset));
    }

    public CompletableFuture<CallTree> treeAsync(String method, Direction dir, int depth, String dataset) {
        return async(() -> tree(method, dir, depth, dataset));
    }

    public CompletableFuture<String> smaliAsync(String method, String dataset) {
        return async(() -> smali(method, dataset));
    }

    public <T> CompletableFuture<T> async(Supplier<T> work) {
        return CompletableFuture.supplyAsync(work, POOL);
    }

    // ------------------------------------------------------------- internals

    private ObjectNode withDataset(ObjectNode body, String dataset) {
        if (dataset != null && !dataset.isBlank()) {
            body.put("dataset", dataset);
        }
        return body;
    }

    /** Applies the accept header and, when configured, the bearer token. */
    private HttpRequest.Builder request(String path) {
        HttpRequest.Builder b = HttpRequest.newBuilder(base.resolve(path))
                .timeout(requestTimeout)
                .header("accept", "application/json");
        if (token != null) {
            b.header("Authorization", "Bearer " + token);
        }
        return b;
    }

    private JsonNode get(String path) {
        return send(request(path).GET().build());
    }

    private JsonNode post(String path, JsonNode body) {
        try {
            HttpRequest req = request(path)
                    .header("content-type", "application/json")
                    .POST(HttpRequest.BodyPublishers.ofString(mapper.writeValueAsString(body), StandardCharsets.UTF_8))
                    .build();
            return send(req);
        } catch (IOException e) {
            throw new DexsqlException("cannot encode request: " + e.getMessage(), 0, e);
        }
    }

    private <T> T post(String path, JsonNode body, Class<T> type) {
        return mapper.convertValue(post(path, body), type);
    }

    private JsonNode send(HttpRequest req) {
        try {
            HttpResponse<String> res = http.send(req, HttpResponse.BodyHandlers.ofString(StandardCharsets.UTF_8));
            if (res.statusCode() >= 400) {
                String message = res.body();
                try {
                    JsonNode err = mapper.readTree(res.body());
                    if (err.hasNonNull("error")) {
                        message = err.get("error").asText();
                    }
                } catch (IOException ignored) {
                    // keep raw body
                }
                throw new DexsqlException(message, res.statusCode(), null);
            }
            return mapper.readTree(res.body());
        } catch (IOException e) {
            throw new DexsqlException("request failed: " + e.getMessage(), 0, e);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            throw new DexsqlException("cancelled", 0, e);
        }
    }

    private static String urlEncode(String s) {
        return java.net.URLEncoder.encode(s, StandardCharsets.UTF_8);
    }
}
