package org.dexsqltree.client;

import org.junit.jupiter.api.Assumptions;
import org.junit.jupiter.api.Test;

import java.nio.file.Files;
import java.nio.file.Path;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertTrue;

/**
 * End-to-end check against the real Go engine. The test is skipped when the
 * binary or the demo APK has not been built, so a clean checkout still passes.
 */
class EngineIntegrationTest {

    @Test
    void queryTreeAndSmaliOverHttp() throws Exception {
        Path bin = Path.of("../../bin/dexsql-tree").toAbsolutePath().normalize();
        Path apk = Path.of("../../testdata/demo.apk").toAbsolutePath().normalize();
        Assumptions.assumeTrue(Files.isExecutable(bin), "engine binary not built: " + bin);
        Assumptions.assumeTrue(Files.exists(apk), "demo apk missing: " + apk);

        try (DexsqlProcess engine = DexsqlProcess.start(bin, apk)) {
            DexsqlClient client = engine.client();
            DatasetInfo d = client.datasets().get(0);
            assertEquals(1, d.stats().classes());
            assertEquals(2, d.stats().methods());

            QueryResult strings = client.query("SELECT value FROM method_strings", 10);
            assertEquals("Hello from DexSQL-Tree", strings.cell(0, 0));

            CallTree tree = client.tree("Ljava/io/PrintStream;->println(Ljava/lang/String;)V",
                    Direction.CALLERS, 2);
            assertEquals("Ldemo/Hello;->main([Ljava/lang/String;)V", tree.root().children().get(0).descriptor());
            assertFalse(tree.truncated());

            String smali = client.smali("Ldemo/Hello;->main([Ljava/lang/String;)V");
            assertTrue(smali.contains("invoke-virtual"));
        }
    }
}
