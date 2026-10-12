package io.nekohasekai.sagernet.group

import io.nekohasekai.sagernet.fmt.xhttp.XHttpBean
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class BlackstoneRawImportTest {
    private fun yaml(type: String, extra: String = ""): String {
        val extraLines = extra.lineSequence().filter { it.isNotEmpty() }
            .joinToString(separator = "\n") { "    $it" }
        return buildString {
            appendLine("proxies:")
            appendLine("  - name: fixture")
            appendLine("    type: $type")
            appendLine("    server: node.example")
            appendLine("    port: 443")
            appendLine("    password: token-fixture")
            appendLine("    node-id: node-fixture")
            if (extraLines.isNotEmpty()) append(extraLines)
        }
    }

    @Test fun canonicalBlackstoneRequiresAndPreservesCompleteDevicePair() = runBlocking {
        val bean = RawUpdater.parseRaw(yaml("Blackstone", "device_id: device-fixture\ndevice_name: tablet-fixture"))
            .orEmpty().single() as XHttpBean
        assertEquals("device-fixture", bean.deviceId)
        assertEquals("tablet-fixture", bean.deviceName)
    }

    @Test fun legacyXhttpMayOmitBothDeviceFields() = runBlocking {
        val bean = RawUpdater.parseRaw(yaml("xhttp")).orEmpty().single() as XHttpBean
        assertEquals("", bean.deviceId)
        assertEquals("", bean.deviceName)
    }

    @Test fun invalidCanonicalAndLegacyDevicePairsAreRejected() = runBlocking {
        val invalid = listOf(
            yaml("Blackstone"),
            yaml("Blackstone", "device_id: device-fixture\ndevice_name: '   '"),
            yaml("Blackstone", "device_id: 123\ndevice_name: tablet-fixture"),
            yaml("xhttp", "device_id: device-fixture"),
            yaml("xhttp", "device_id: ''\ndevice_name: tablet-fixture"),
            yaml("xhttp", "device_id: [bad]\ndevice_name: tablet-fixture"),
        )
        invalid.forEach { raw ->
            assertTrue(runCatching { RawUpdater.parseRaw(raw) }.isFailure)
        }
    }

    @Test fun exactProtocolNamesOnly() = runBlocking {
        val canonical = RawUpdater.parseRaw(yaml("Blackstone", "device_id: device-fixture\ndevice_name: tablet-fixture")).orEmpty()
        val legacy = RawUpdater.parseRaw(yaml("xhttp")).orEmpty()
        val lowerCanonical = RawUpdater.parseRaw(yaml("blackstone", "device_id: device-fixture\ndevice_name: tablet-fixture")).orEmpty()
        val mixedLegacy = RawUpdater.parseRaw(yaml("XHttp")).orEmpty()
        assertEquals(1, canonical.size)
        assertEquals(1, legacy.size)
        assertTrue(lowerCanonical.isEmpty())
        assertTrue(mixedLegacy.isEmpty())
    }
}
