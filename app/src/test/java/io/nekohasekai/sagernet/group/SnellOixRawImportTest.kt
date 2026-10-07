package io.nekohasekai.sagernet.group

import io.nekohasekai.sagernet.fmt.snell.SnellBean
import io.nekohasekai.sagernet.fmt.snell.buildSingBoxOutboundSnellBean
import kotlinx.coroutines.runBlocking
import org.junit.Assert.*
import org.junit.Test

class SnellOixRawImportTest {
    private val yaml = """
        proxies:
          - name: synthetic Oix
            type: snell
            server: node.example
            port: 443
            psk: synthetic-key
            version: 4
            obfs-opts: {mode: ech-tls, identity-version: 2, preconnect: 3, legacy-fallback: true, sni: inner.example, ech-config: AQID}
          - name: ordinary
            type: snell
            server: ordinary.example
            port: 8443
            psk: ordinary-key
            version: 5
    """.trimIndent()

    @Test fun clipboardAndFileUseSameRawParserAndPreserveMixedNodes() = runBlocking {
        for (fileName in listOf("", "nodes.yaml", "nodes.yml")) {
            val beans = RawUpdater.parseRaw(yaml, fileName).orEmpty()
            assertEquals(2, beans.size)
            val oix = beans[0] as SnellBean
            assertTrue(oix.oixEchTls)
            assertEquals("synthetic-key", oix.psk)
            assertEquals(3, oix.oixPreconnect)
            assertTrue(oix.oixLegacyFallback)
            assertEquals(true, buildSingBoxOutboundSnellBean(oix).oix_ech)
            assertFalse((beans[1] as SnellBean).oixEchTls)
            assertEquals(5, (beans[1] as SnellBean).version)
        }
    }

    @Test fun utf8BomFileStillUsesYamlParser() = runBlocking {
        val beans = RawUpdater.parseRaw("\uFEFF" + yaml, "nodes.yaml").orEmpty()
        assertEquals(2, beans.size)
        assertTrue((beans[0] as SnellBean).oixEchTls)
    }
}
