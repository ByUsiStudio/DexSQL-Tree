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

/**
 * Blocking client for the dexsql-tree HTTP API.
 *
 * <p>The client is thread-safe: it holds no mutable state other than the
 * currently selected dataset id, which callers set explicitly.
 */
public final class DexsqlClient {
    private final URI base;
    private final HttpClient http;
    private final ObjectMapper mapper = new ObjectMapper();
    private final Duration requestTimeout;
    private volatile String datasetId;

    public DexsqlClient(String baseUrl) {
        this(baseUrl, Duration.ofMinutes(5));
    }

    public DexsqlClient(String baseUrl, Duration requestTimeout) {
        String url = baseUrl.endsWith("/") ? baseUrl.substring(0, baseUrl.length() - 1) : baseUrl;
        this.base = URI.create(url);
        this.requestTimeout = requestTimeout;
        this.http = HttpClient.newBuilder()
                .connectTimeout(Duration.ofSeconds(5))
                .followRedirects(HttpClient.Redirect.NORMAL)
                .build();
    }

    public String baseUrl() {
        return base.toString();
    }

    /** The dataset used by query/tree/smali when no explicit id is supplied. */
    public String datasetId() {
        return datasetId;
    }

    public void useDataset(String id) {
        this.datasetId = id;
    }

    public boolean health() {
        try {
            JsonNode node = get("/api/health");
            return node.path("ok").asBoolean(false);
        } catch (RuntimeException e) {
            return false;
        }
    }

    public List<DatasetInfo> datasets() {
        JsonNode node = get("/api/datasets");
        List<DatasetInfo> out = new ArrayList<>();
        for (JsonNode d : node.path("datasets")) {
            out.add(mapper.convertValue(d, DatasetInfo.class));
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

    public List<TableInfo> tables() {
        JsonNode node = get("/api/tables");
        List<TableInfo> out = new ArrayList<>();
        for (JsonNode t : node.path("tables")) {
            out.add(mapper.convertValue(t, TableInfo.class));
        }
        return out;
    }

    public QueryResult query(String sql, int limit) {
        ObjectNode body = mapper.createObjectNode();
        body.put("sql", sql);
        body.put("limit", limit);
        if (datasetId != null) {
            body.put("dataset", datasetId);
        }
        return post("/api/query", body, QueryResult.class);
    }

    public QueryResult query(String sql) {
        return query(sql, 1000);
    }

    public CallTree tree(String method, Direction direction, int depth) {
        ObjectNode body = mapper.createObjectNode();
        body.put("method", method);
        body.put("direction", direction.wire());
        body.put("depth", depth);
        if (datasetId != null) {
            body.put("dataset", datasetId);
        }
        return post("/api/tree", body, CallTree.class);
    }

    public String smali(String method) {
        String url = "/api/smali?method=" + urlEncode(method)
                + (datasetId == null ? "" : "&dataset=" + urlEncode(datasetId));
        return get(url).path("smali").asText();
    }

    private JsonNode get(String path) {
        HttpRequest req = HttpRequest.newBuilder(base.resolve(path))
                .timeout(requestTimeout)
                .header("accept", "application/json")
                .GET()
                .build();
        return send(req);
    }

    private JsonNode post(String path, JsonNode body) {
        try {
            HttpRequest req = HttpRequest.newBuilder(base.resolve(path))
                    .timeout(requestTimeout)
                    .header("content-type", "application/json")
                    .header("accept", "application/json")
                    .POST(HttpRequest.BodyPublishers.ofString(mapper.writeValueAsString(body), StandardCharsets.UTF_8))
                    .build();
            return send(req);
        } catch (IOException e) {
            throw new DexsqlException("cannot encode request: " + e.getMessage(), 0, e);
        }
    }

    private <T> T post(String path, JsonNode body, Class<T> type) {
        JsonNode node = post(path, body);
        return mapper.convertValue(node, type);
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
            throw new DexsqlException("interrupted", 0, e);
        }
    }

    private static String urlEncode(String s) {
        return java.net.URLEncoder.encode(s, StandardCharsets.UTF_8);
    }
}
