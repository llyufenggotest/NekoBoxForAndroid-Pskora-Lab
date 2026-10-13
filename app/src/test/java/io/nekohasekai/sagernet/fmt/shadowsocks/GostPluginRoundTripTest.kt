package io.nekohasekai.sagernet.fmt.shadowsocks

import com.esotericsoftware.kryo.io.ByteBufferInput
import com.esotericsoftware.kryo.io.ByteBufferOutput
import com.google.gson.Gson
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [28])
class GostPluginRoundTripTest {
    @Test fun codecEscapesDelimitersAndRejectsAmbiguousInput() {
        val raw = "mode=ws;path=/gost;future=value\\;semi\\=equal\\\\slash;nocomp"
        val parsed = GostPluginOptions.parse(raw)
        assertEquals("value;semi=equal\\slash", parsed.value("future"))
        assertTrue(parsed.flag("nocomp"))
        assertEquals("mode=ws;path=/gost;nocomp;future=value\\;semi\\=equal\\\\slash", parsed.canonical())
        assertThrows(IllegalArgumentException::class.java) { GostPluginOptions.parse("mode=ws\\") }
        assertThrows(IllegalArgumentException::class.java) { GostPluginOptions.parse("=empty") }
        assertThrows(IllegalArgumentException::class.java) { GostPluginOptions.parse("mode=ws;mode=wss") }
    }

    @Test fun beanKryoCloneUriJsonAndNativeOutboundRemainLossless() {
        val plugin = "gost-plugin;mode=wss;host=cdn.invalid;path=/gost;serverName=edge.invalid;" +
            "insecure;nocomp;ed=64;mux=3;future=value\\;semi\\=equal\\\\slash"
        val bean = ShadowsocksBean().apply {
            initializeDefaultValues()
            serverAddress = "2001:db8::12"
            serverPort = 443
            method = "aes-256-gcm"
            password = "test-only-password"
            name = "gost fixture + %25"
            this.plugin = plugin
            sUoT = true
        }

        val output = ByteBufferOutput(8192)
        bean.serialize(output)
        val stored = ShadowsocksBean().apply { deserialize(ByteBufferInput(output.toBytes())) }
        val cloned = bean.clone()
        val shared = parseShadowsocks(bean.toUri())
        val json = JSONObject().apply {
            put("server", bean.serverAddress)
            put("server_port", bean.serverPort)
            put("method", bean.method)
            put("password", bean.password)
            put("plugin", "gost-plugin")
            put("plugin_opts", plugin.substringAfter(';'))
            put("remarks", bean.name)
        }.parseShadowsocks()

        listOf(stored, cloned, shared, json).forEach { candidate ->
            assertEquals(plugin, candidate.plugin)
            assertEquals(bean.serverAddress, candidate.serverAddress)
            assertEquals(bean.serverPort, candidate.serverPort)
        }
        assertEquals(bean.name, shared.name)

        val outbound = buildSingBoxOutboundShadowsocksBean(shared)
        assertEquals("gost-plugin", outbound.plugin)
        assertEquals(plugin.substringAfter(';'), outbound.plugin_opts)
        val native = Gson().toJsonTree(outbound).asJsonObject
        assertEquals("gost-plugin", native["plugin"].asString)
        assertEquals(plugin.substringAfter(';'), native["plugin_opts"].asString)
    }

    @Test fun ordinaryPluginsAreNotCanonicalizedAsGost() {
        val plugin = "v2ray-plugin;mode=websocket;host=cdn.invalid;path=/v;mux=8"
        val bean = ShadowsocksBean().apply {
            initializeDefaultValues()
            serverAddress = "node.invalid"
            serverPort = 443
            method = "aes-128-gcm"
            password = "test-only-password"
            this.plugin = plugin
        }
        assertEquals(plugin, parseShadowsocks(bean.toUri()).plugin)
        assertFalse(buildSingBoxOutboundShadowsocksBean(bean).plugin_opts.isNullOrBlank())
    }

    @Test fun uotBooleanIsEmittedAsExplicitVersionTwoObject() {
        val bean = ShadowsocksBean().apply {
            initializeDefaultValues()
            serverAddress = "192.0.2.20"
            serverPort = 443
            method = "aes-256-gcm"
            password = "test-only-password"
            sUoT = true
        }
        val outbound = buildSingBoxOutboundShadowsocksBean(bean)
        assertEquals(true, outbound.udp_over_tcp.enabled)
        assertEquals(2, outbound.udp_over_tcp.version)
    }
}
