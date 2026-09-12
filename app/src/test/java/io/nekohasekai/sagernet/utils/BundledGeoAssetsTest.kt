package io.nekohasekai.sagernet.utils

import org.junit.Assert.*
import org.junit.Test
import java.io.File
import java.nio.file.Files

class BundledGeoAssetsTest {
    @Test fun installsRealBundledAssetsAndPreservesCustomFiles() {
        val directory = Files.createTempDirectory("geo-assets-test").toFile()
        val assets = File("src/main/assets")
        try {
            BundledGeoAssets.ensure(directory) { File(assets, it).inputStream() }
            val geoipLength = File(directory, "geoip.db").length()
            val geositeLength = File(directory, "geosite.db").length()
            assertTrue(geoipLength > 1_000_000L)
            assertTrue(geositeLength > 1_000_000L)
            File(directory, "geoip.db").writeText("custom")
            File(directory, "geosite.db").writeBytes(byteArrayOf())
            BundledGeoAssets.ensure(directory) { File(assets, it).inputStream() }
            assertEquals("custom", File(directory, "geoip.db").readText())
            assertEquals(geositeLength, File(directory, "geosite.db").length())
            assertFalse(File(directory, "geosite.db.installing").exists())
        } finally { directory.deleteRecursively() }
    }
}
