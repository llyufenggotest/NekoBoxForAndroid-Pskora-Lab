package io.nekohasekai.sagernet.fmt.snell

import com.esotericsoftware.kryo.io.ByteBufferInput
import com.esotericsoftware.kryo.io.ByteBufferOutput
import org.junit.Assert.*
import org.junit.Test
import org.yaml.snakeyaml.Yaml

/** Synthetic credentials only; exercise the same SnakeYAML scalar types as RawUpdater. */
class SnellOixYamlTest {
    private fun parse(mode: String = "ech-tls"): SnellBean {
        val yaml = """
            proxies:
              - name: example Oix
                type: snell
                server: node.example
                port: 8443
                psk: synthetic-key
                version: 4
                udp: true
                obfs-opts:
                  mode: $mode
                  identity-version: 2
                  alpn: snell-ech/1
                  legacy-fallback: true
                  preconnect: 3
                  sni: inner.example
                  ech-config: AQID
        """.trimIndent()
        val root = Yaml().load<Map<String, Any?>>(yaml)
        @Suppress("UNCHECKED_CAST")
        return parseClashSnell((root["proxies"] as List<Map<String, Any?>>).single())
    }

    @Test fun officialModeEnablesIndependentOixFields() {
        val bean = parse()
        assertTrue(bean.oixEchTls)
        assertEquals(8443, bean.serverPort)
        assertEquals("synthetic-key", bean.psk)
        assertEquals(2, bean.oixIdentityVersion)
        assertEquals(3, bean.oixPreconnect)
        assertTrue(bean.oixLegacyFallback)
        assertEquals("inner.example", bean.oixSni)
        assertEquals("AQID", bean.oixConfig)
        val outbound = buildSingBoxOutboundSnellBean(bean)
        assertEquals(true, outbound.oix_ech)
        assertEquals(3, outbound.oix_preconnect)
    }

    @Test fun legacyAliasRemainsAccepted() { assertTrue(parse("oix-ech-tls").oixEchTls) }

    @Test fun disablingOixCannotLeakInternalMarkerToOrdinarySnell() {
        val bean = parse("oix-ech-tls")
        bean.oixEchTls = false
        val outbound = buildSingBoxOutboundSnellBean(bean)
        assertNull(outbound.oix_ech)
        assertTrue(outbound.obfs_mode.isNullOrEmpty())
    }

    @Test fun ordinaryTlsNeverEnablesOix() { assertFalse(parse("tls").oixEchTls) }

    @Test fun scalarStringsAndSnellSixAreNotSilentlyDropped() {
        val bean = parseClashSnell(mapOf("server" to "node.example", "port" to "8443",
            "psk" to "synthetic-key", "version" to "6", "userkey" to "synthetic-user",
            "mode" to "unshaped", "quic-proxy-mode" to true, "udp" to "true"))
        assertEquals(8443, bean.serverPort)
        assertEquals(6, bean.version)
        assertEquals("synthetic-user", bean.userKey)
        assertEquals("unshaped", bean.mode)
        assertTrue(bean.quicProxyMode)
        assertEquals("", bean.network)
        assertFalse(bean.oixEchTls)
    }

    @Test fun explicitUriParametersPreserveOixWithoutPasswordSuffix() {
        val bean = parse()
        val restored = parseSnell(bean.toUri()).apply { initializeDefaultValues() }
        assertEquals(bean.psk, restored.psk)
        assertEquals(bean.name, restored.name)
        assertEquals(bean.oixEchTls, restored.oixEchTls)
        assertEquals(bean.oixIdentityVersion, restored.oixIdentityVersion)
        assertEquals(bean.oixAlpn, restored.oixAlpn)
        assertEquals(bean.oixPreconnect, restored.oixPreconnect)
        assertEquals(bean.oixLegacyFallback, restored.oixLegacyFallback)
        assertEquals(bean.oixSni, restored.oixSni)
        assertEquals(bean.oixConfig, restored.oixConfig)
    }

    @Test fun beanSerializationRetainsAllOixFields() {
        val bean = parse()
        val output = ByteBufferOutput(4096)
        bean.serialize(output)
        val restored = SnellBean().apply { deserialize(ByteBufferInput(output.toBytes())) }
        assertEquals(bean.oixEchTls, restored.oixEchTls)
        assertEquals(bean.oixIdentityVersion, restored.oixIdentityVersion)
        assertEquals(bean.oixAlpn, restored.oixAlpn)
        assertEquals(bean.oixPreconnect, restored.oixPreconnect)
        assertEquals(bean.oixLegacyFallback, restored.oixLegacyFallback)
        assertEquals(bean.oixSni, restored.oixSni)
        assertEquals(bean.oixConfig, restored.oixConfig)
    }
}
