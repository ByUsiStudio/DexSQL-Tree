package org.dexsqltree.client;

import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpServer;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;

import java.io.IOException;
import java.io.OutputStream;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;
import java.nio.file.Path;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertNull;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

class DexsqlClientTest {
    private HttpServer server;
    private DexsqlClient client;

    @BeforeEach
    void setUp() throws IOException {
        server = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
        server.createContext("/api/health", ex -> respond(ex, 200, "{\"ok\":true,\"version\":\"0.1.0\"}"));
        server.createContext("/api/datasets", ex -> respond(ex, 200,
                "{\"id\":\"d1\",\"path\":\"/tmp/demo.apk\",\"stats\":{\"files\":1,\"dexBytes\":672,"
                        + "\"strings\":11,\"types\":6,\"methods\":2,\"classes\":1,\"fields\":1,"
                        + "\"codeItems\":1,\"warnings\":[]}}"));
        server.createContext("/api/tables", ex -> respond(ex, 200,
                "{\"tables\":[{\"name\":\"methods\",\"cols\":[\"file_id UInt32\",\"descriptor Utf8\"],"
                        + "\"doc\":\"All referenced methods\"}]}"));
        server.createContext("/api/query", ex -> {
            String body = new String(ex.getRequestBody().readAllBytes(), StandardCharsets.UTF_8);
            if (body.contains("boom")) {
                respond(ex, 400, "{\"error\":\"no such column: boom\"}");
            } else {
                respond(ex, 200, "{\"columns\":[\"value\",\"n\"],\"rows\":[[\"Hello\",3],[null,4]],"
                        + "\"rowCount\":2,\"truncated\":false,\"elapsedMs\":1}");
            }
        });
        server.createContext("/api/tree", ex -> respond(ex, 200,
                "{\"root\":{\"descriptor\":\"La;->b()V\",\"class\":\"La;\",\"name\":\"b\",\"fileId\":0,"
                        + "\"methodIdx\":1,\"depth\":0,\"calls\":0,"
                        + "\"children\":[{\"descriptor\":\"La;->c()V\",\"class\":\"La;\",\"name\":\"c\","
                        + "\"fileId\":0,\"methodIdx\":2,\"depth\":1,\"calls\":2,\"cycle\":false}]},"
                        + "\"direction\":\"callees\",\"maxDepth\":2,\"nodeCount\":2,\"edgeCount\":1,"
                        + "\"truncated\":false}"));
        server.createContext("/api/smali", ex -> respond(ex, 200, "{\"smali\":\".method La;->b()V\\n.end method\"}"));
        server.start();
        client = new DexsqlClient("http://127.0.0.1:" + server.getAddress().getPort());
    }

    @AfterEach
    void tearDown() {
        server.stop(0);
    }

    private static void respond(HttpExchange ex, int code, String body) throws IOException {
        byte[] bytes = body.getBytes(StandardCharsets.UTF_8);
        ex.getResponseHeaders().add("content-type", "application/json");
        ex.sendResponseHeaders(code, bytes.length);
        try (OutputStream out = ex.getResponseBody()) {
            out.write(bytes);
        }
    }

    @Test
    void healthAndOpenDataset() {
        assertTrue(client.health());
        DatasetInfo d = client.openDataset(Path.of("/tmp/demo.apk"));
        assertEquals("d1", d.id());
        assertEquals(1, d.stats().classes());
        assertEquals("d1", client.datasetId());
    }

    @Test
    void queryKeepsNullsAndNumbers() {
        QueryResult r = client.query("SELECT value, n FROM strings", 10);
        assertEquals(2, r.columns().size());
        assertEquals(2, r.rowCount());
        assertEquals("Hello", r.cell(0, 0));
        assertEquals(3L, ((Number) r.cell(0, 1)).longValue());
        assertNull(r.cell(1, 0));
        assertFalse(r.truncated());
    }

    @Test
    void structuredErrorBecomesException() {
        DexsqlException e = assertThrows(DexsqlException.class, () -> client.query("SELECT boom"));
        assertEquals(400, e.status());
        assertTrue(e.getMessage().contains("no such column"));
    }

    @Test
    void treeMapsClassNameProperty() {
        CallTree t = client.tree("La;->b()V", Direction.CALLEES, 2);
        assertEquals("La;", t.root().className());
        assertEquals(1, t.root().children().size());
        assertEquals(2, t.root().children().get(0).calls());
        assertFalse(t.root().children().get(0).cycle());
    }

    @Test
    void tablesAndSmali() {
        assertEquals(1, client.tables().size());
        assertEquals("methods", client.tables().get(0).name());
        assertTrue(client.smali("La;->b()V").contains(".method"));
    }
}
