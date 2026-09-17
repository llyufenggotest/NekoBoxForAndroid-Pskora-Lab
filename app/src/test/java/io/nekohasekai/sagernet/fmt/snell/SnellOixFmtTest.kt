package io.nekohasekai.sagernet.fmt.snell

import com.google.gson.Gson
import io.nekohasekai.sagernet.fmt.KryoConverters
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class SnellOixFmtTest {
    @Test
    fun clashMarkerCarriesOixFields() {
        val bean = parseClashSnell(mapOf(
            "name" to "OIX",
            "server" to "node.example",
            "port" to 443,
            "psk" to "psk",
            "version" to 4,
            "obfs-opts" to mapOf(
                "mode" to "oix-ech-tls",
                "sni" to "inner.example",
                "ech-config" to "AQID",
                "alpn" to "snell-ech/1",
                "identity-version" to 2,
                "legacy-fallback" to true,
                "preconnect" to 3,
            ),
        ))

        assertTrue(bean.oixEchTls)
        assertEquals("oix-ech-tls", bean.obfsMode)
        assertEquals("inner.example", bean.oixSni)
        assertEquals("AQID", bean.oixConfig)
        assertEquals(2, bean.oixIdentityVersion)
        assertEquals("snell-ech/1", bean.oixAlpn)
        assertTrue(bean.oixLegacyFallback)
        assertEquals(3, bean.oixPreconnect)
    }

    @Test
    fun ordinarySnellDoesNotAcquireOixFields() {
        val bean = parseClashSnell(mapOf("server" to "node.example", "port" to 443, "psk" to "psk"))
        assertFalse(bean.oixEchTls)
        assertEquals("", bean.obfsMode)
    }

    @Test
    fun builderCarriesOixJsonFieldsWithoutChangingMarker() {
        val bean = parseClashSnell(mapOf(
            "server" to "node.example", "port" to 443, "psk" to "psk", "version" to 4,
            "obfs-opts" to mapOf("mode" to "oix-ech-tls", "sni" to "inner.example", "ech-config" to "AQID"),
        ))
        val outbound = buildSingBoxOutboundSnellBean(bean)
        val json = Gson().toJson(outbound)
        assertTrue(json.contains("\"oix_ech\":true"))
        assertTrue(json.contains("\"obfs_mode\":\"oix-ech-tls\""))
        assertTrue(json.contains("\"oix_sni\":\"inner.example\""))
        assertTrue(json.contains("\"oix_config\":\"AQID\""))
    }

    @Test
    fun oixFieldsSurviveBeanSerialization() {
        val bean = parseClashSnell(mapOf(
            "server" to "node.example", "port" to 443, "psk" to "psk",
            "obfs-opts" to mapOf("mode" to "oix-ech-tls", "sni" to "inner.example", "ech-config" to "AQID"),
        ))
        val restored = KryoConverters.snellDeserialize(KryoConverters.serialize(bean))
        assertEquals(bean.oixEchTls, restored.oixEchTls)
        assertEquals(bean.oixSni, restored.oixSni)
        assertEquals(bean.oixConfig, restored.oixConfig)
    }
}
