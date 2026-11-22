package org.dexsqltree.client;

import java.io.IOException;
import java.net.ServerSocket;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.time.Duration;
import java.util.ArrayList;
import java.util.List;
import java.util.Locale;
import java.util.Optional;

/**
 * Owns a local {@code dexsql-tree serve} process.
 *
 * <p>The binary is resolved from, in order: the {@code dexsql.tree.bin} system
 * property, the {@code DEXSQL_TREE_BIN} environment variable, {@code ./dexsql-tree},
 * or {@code dexsql-tree} on {@code PATH}.
 */
public final class DexsqlProcess implements AutoCloseable {
    private final Process process;
    private final DexsqlClient client;
    private final int port;
    private final Path logFile;

    private DexsqlProcess(Process process, DexsqlClient client, int port, Path logFile) {
        this.process = process;
        this.client = client;
        this.port = port;
        this.logFile = logFile;
    }

    public DexsqlClient client() {
        return client;
    }

    public int port() {
        return port;
    }

    public Path logFile() {
        return logFile;
    }

    public static Optional<Path> locateBinary() {
        String prop = System.getProperty("dexsql.tree.bin");
        if (prop != null && !prop.isBlank() && Files.isExecutable(Path.of(prop))) {
            return Optional.of(Path.of(prop));
        }
        String env = System.getenv("DEXSQL_TREE_BIN");
        if (env != null && !env.isBlank() && Files.isExecutable(Path.of(env))) {
            return Optional.of(Path.of(env));
        }
        Path local = Path.of("dexsql-tree");
        if (Files.isExecutable(local)) {
            return Optional.of(local.toAbsolutePath());
        }
        String path = System.getenv("PATH");
        if (path != null) {
            String exe = isWindows() ? "dexsql-tree.exe" : "dexsql-tree";
            for (String dir : path.split(java.io.File.pathSeparator)) {
                Path candidate = Path.of(dir, exe);
                if (Files.isExecutable(candidate)) {
                    return Optional.of(candidate);
                }
            }
        }
        return Optional.empty();
    }

    private static boolean isWindows() {
        return System.getProperty("os.name", "").toLowerCase(Locale.ROOT).contains("win");
    }

    /** Starts the engine on a free loopback port and waits until it answers. */
    public static DexsqlProcess start(Path binary, Path input) throws IOException {
        return start(binary, input, 0, Duration.ofSeconds(30));
    }

    public static DexsqlProcess start(Path binary, Path input, int requestedPort, Duration startupTimeout)
            throws IOException {
        int port = requestedPort > 0 ? requestedPort : freePort();
        Path log = Files.createTempFile("dexsql-tree-", ".log");
        List<String> cmd = new ArrayList<>();
        cmd.add(binary.toAbsolutePath().toString());
        cmd.add("serve");
        cmd.add("--input");
        cmd.add(input.toAbsolutePath().toString());
        cmd.add("--addr");
        cmd.add("127.0.0.1:" + port);
        ProcessBuilder pb = new ProcessBuilder(cmd);
        pb.redirectErrorStream(true);
        pb.redirectOutput(log.toFile());
        Process p = pb.start();
        DexsqlClient client = new DexsqlClient("http://127.0.0.1:" + port);
        long deadline = System.nanoTime() + startupTimeout.toNanos();
        while (System.nanoTime() < deadline) {
            if (client.health()) {
                return new DexsqlProcess(p, client, port, log);
            }
            if (!p.isAlive()) {
                throw new IOException("dexsql-tree exited during startup; log: " + log + tail(log));
            }
            try {
                Thread.sleep(200);
            } catch (InterruptedException e) {
                Thread.currentThread().interrupt();
                p.destroyForcibly();
                throw new IOException("interrupted while starting dexsql-tree", e);
            }
        }
        p.destroyForcibly();
        throw new IOException("dexsql-tree did not become ready in " + startupTimeout.toSeconds() + "s; log: " + log);
    }

    private static int freePort() throws IOException {
        try (ServerSocket s = new ServerSocket(0)) {
            s.setReuseAddress(true);
            return s.getLocalPort();
        }
    }

    private static String tail(Path log) {
        try {
            List<String> lines = Files.readAllLines(log, StandardCharsets.UTF_8);
            int from = Math.max(0, lines.size() - 20);
            return "\n" + String.join("\n", lines.subList(from, lines.size()));
        } catch (IOException e) {
            return "";
        }
    }

    @Override
    public void close() {
        process.destroy();
        try {
            if (!process.waitFor(5, java.util.concurrent.TimeUnit.SECONDS)) {
                process.destroyForcibly();
            }
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            process.destroyForcibly();
        }
    }
}
