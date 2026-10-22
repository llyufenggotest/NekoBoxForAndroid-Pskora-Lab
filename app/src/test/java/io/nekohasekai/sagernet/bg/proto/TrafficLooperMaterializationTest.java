package io.nekohasekai.sagernet.bg.proto;

import java.io.File;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.concurrent.TimeUnit;
import org.junit.Test;
import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertTrue;

/** Required host-fixture regression, not an Android/Binder/native integration claim. */
public class TrafficLooperMaterializationTest {
    @Test
    public void completeProductionSourcesPreserveSamplingWithoutBackgroundUiDtos() throws Exception {
        File root = new File(System.getProperty("user.dir")).getCanonicalFile();
        while (root != null && !new File(root, "tools/positive_a01_gate/run.py").isFile()) {
            root = root.getParentFile();
        }
        assertTrue("Cannot locate checkout A01 regression gate", root != null);
        String scratch = System.getenv("RUNNER_TEMP");
        if (scratch == null || scratch.isEmpty()) scratch = System.getenv("TMPDIR");
        if (scratch == null || scratch.isEmpty()) scratch = System.getProperty("java.io.tmpdir");
        Path outputParent = Files.createTempDirectory(new File(scratch).toPath(), "positive-a01-");
        Path output = outputParent.resolve("gate");
        Path log = outputParent.resolve("launcher.log");
        boolean windows = System.getProperty("os.name").startsWith("Windows");
        String python = System.getenv("A01_PYTHON");
        if (python == null || python.isEmpty()) python = windows ? "python" : "python3";
        String java = new File(System.getProperty("java.home"), windows ? "bin/java.exe" : "bin/java").getPath();
        ProcessBuilder builder = new ProcessBuilder(python,
            new File(root, "tools/positive_a01_gate/run.py").getPath(),
            "--source-root", root.getPath(), "--output", output.toString(), "--java", java);
        builder.directory(root);
        builder.environment().put("PYTHONDONTWRITEBYTECODE", "1");
        builder.redirectErrorStream(true).redirectOutput(log.toFile());
        Process process = builder.start();
        if (!process.waitFor(10, TimeUnit.MINUTES)) {
            process.destroyForcibly();
            throw new AssertionError("A01 fixture timeout; diagnostics: " + outputParent);
        }
        String diagnostics = new String(Files.readAllBytes(log), StandardCharsets.UTF_8);
        assertEquals("A01 production fixture failed; diagnostics: " + outputParent + "\n" + diagnostics,
            0, process.exitValue());
        assertTrue("A01 gate did not complete: " + diagnostics,
            diagnostics.contains("A01 PASS: budget, 22 lifecycle scenarios, 21 semantic oracles"));
    }
}
