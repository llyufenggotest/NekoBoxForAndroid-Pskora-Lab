package io.nekohasekai.sagernet.fmt.snell

import com.esotericsoftware.kryo.io.ByteBufferInput
import com.esotericsoftware.kryo.io.ByteBufferOutput
import org.junit.Assert.*
import org.junit.Test

class SnellLosslessRoundTripTest {
    private fun source() = mapOf<String, Any?>(
        "name" to "中文 + / %25 # 节点", "server" to "2001:db8::1", "port" to 8443,
        "psk" to "synthetic+/%25=?#", "identity" to true, "tfo" to true,
        "reuse" to true, "udp" to true, "version" to 4,
        "obfs-opts" to mapOf("mode" to "ech-tls", "path" to "/路径?x=+&y=%25#z",
            "skip-cert-verify" to true, "sni" to "inner.example", "ech-config" to "AB+/==",
            "alpn" to "snell-ech/1", "identity-version" to 2, "legacy-fallback" to true, "preconnect" to 3))

    @Test fun legacyBeanVersionsOneThroughFiveRemainReadable() {
        for (format in 1..5) {
            val output = ByteBufferOutput(8192)
            output.writeInt(format)
            output.writeString("legacy.example")
            output.writeInt(443)
            output.writeString("synthetic-legacy")
            output.writeInt(4)
            output.writeString("http")
            output.writeString("front.example")
            output.writeBoolean(true)
            if (format >= 2) output.writeString("tcp")
            if (format >= 3) { output.writeString("synthetic-user"); output.writeString("default") }
            if (format >= 4) output.writeBoolean(false)
            if (format >= 5) {
                output.writeBoolean(true); output.writeInt(2); output.writeString("snell-ech/1")
                output.writeBoolean(true); output.writeInt(3); output.writeString("inner.example"); output.writeString("AB+/==")
            }
            val restored = SnellBean().apply { deserialize(ByteBufferInput(output.toBytes())) }
            assertEquals("synthetic-legacy", restored.psk)
            assertEquals("legacy.example", restored.serverAddress)
            assertEquals(format >= 5, restored.oixEchTls)
            // Formats 1-5 never stored identity: migrate to exporter-compatible true.
            assertTrue(restored.identity)
            if (format == 5) assertEquals(true, buildSingBoxOutboundSnellBean(restored).oix_identity)
            assertEquals("", restored.oixPath)
            assertFalse(restored.oixSkipCertVerify)
            assertFalse(restored.tcpFastOpen)
        }
    }

    @Test fun ordinarySnellUserKeyAndObfsValuesEncodeSymmetrically() {
        val bean = SnellBean().apply {
            initializeDefaultValues(); serverAddress = "2001:db8::1"; version = 3
            psk = "synthetic+/%25"; userKey = "user+/%25中文"; name = "普通节点 + / %25"
            obfsMode = "http"; obfsHost = "front.example/路径?a=+&b=%25"
        }
        val restored = parseSnell(bean.toUri())
        assertEquals(bean.psk, restored.psk)
        assertEquals(bean.userKey, restored.userKey)
        assertEquals(bean.obfsHost, restored.obfsHost)
        assertEquals(bean.name, restored.name)
        assertFalse(restored.oixEchTls)
        assertNull(buildSingBoxOutboundSnellBean(restored).oix_ech)
    }

    @Test fun missingIdentityDefaultsToExporterAcrossImportStorageShareAndApi() {
        val yaml = parseClashSnell(source() - "identity")
        val uri = parseSnell("snell://synthetic@default.example:443?oix-ech=true")
        val fresh = SnellBean().apply { initializeDefaultValues(); oixEchTls = true }
        for (bean in listOf(yaml, uri, fresh)) {
            assertTrue(bean.identity)
            assertFalse(bean.toUri().contains("identity="))
            val output = ByteBufferOutput(8192)
            bean.serialize(output)
            val stored = SnellBean().apply { deserialize(ByteBufferInput(output.toBytes())) }
            for (candidate in listOf(bean, stored, parseSnell(bean.toUri()))) {
                assertTrue(candidate.identity)
                assertFalse(candidate.toUri().contains("identity="))
                assertEquals(true, buildSingBoxOutboundSnellBean(candidate).oix_identity)
            }
        }
    }

    @Test fun explicitFalseSurvivesEveryBoundaryAndDisabledOixDoesNotEmitIdentity() {
        val bean = parseClashSnell(source() + ("identity" to false))
        val output = ByteBufferOutput(8192)
        bean.serialize(output)
        val stored = SnellBean().apply { deserialize(ByteBufferInput(output.toBytes())) }
        for (candidate in listOf(bean, stored, parseSnell(bean.toUri()))) {
            assertFalse(candidate.identity)
            assertTrue(candidate.toUri().contains("identity=false"))
            assertEquals(false, buildSingBoxOutboundSnellBean(candidate).oix_identity)
            candidate.oixEchTls = false
            assertNull(buildSingBoxOutboundSnellBean(candidate).oix_identity)
            val shared = parseSnell(candidate.toUri())
            assertFalse(shared.oixEchTls)
            assertFalse(shared.identity)
            candidate.oixEchTls = true
            assertEquals(false, buildSingBoxOutboundSnellBean(candidate).oix_identity)
        }
    }

    @Test fun ordinaryMissingIdentityDoesNotAddOixMetadataWhenEnabledLater() {
        val bean = parseSnell("snell://synthetic@ordinary.example:443?version=3&obfs-mode=http")
        assertFalse(bean.toUri().contains("oix-"))
        assertFalse(bean.toUri().contains("identity="))
        assertNull(buildSingBoxOutboundSnellBean(bean).oix_identity)
        bean.oixEchTls = true
        assertEquals(true, buildSingBoxOutboundSnellBean(bean).oix_identity)
    }

    @Test fun identityPresenceAndJsonBooleanContractCoverMissingTrueFalseAndEditorFalse() {
        for (value in listOf<Boolean?>(null, true, false)) {
            val yaml = """type: snell
                |server: synthetic.example
                |port: 443
                |psk: synthetic
                |obfs-opts: {mode: ech-tls}
                |${if (value == null) "" else "identity: $value"}
            """.trimMargin()
            val bean = parseClashSnell(org.yaml.snakeyaml.Yaml().load<Map<String, Any?>>(yaml))
            assertEquals(value != null, bean.identityPresent)
            val output = ByteBufferOutput(8192)
            bean.serialize(output)
            val stored = SnellBean().apply { deserialize(ByteBufferInput(output.toBytes())) }
            for (candidate in listOf(bean, stored, parseSnell(bean.toUri()))) {
                assertEquals(value != null, candidate.identityPresent)
                val json = com.google.gson.Gson().toJsonTree(buildSingBoxOutboundSnellBean(candidate)).asJsonObject
                assertTrue(json["oix_identity"].asJsonPrimitive.isBoolean)
                assertEquals(value ?: true, json["oix_identity"].asBoolean)
                candidate.oixEchTls = false
                assertFalse(com.google.gson.Gson().toJsonTree(buildSingBoxOutboundSnellBean(candidate)).asJsonObject.has("oix_identity"))
            }
        }
        val editor = SnellBean().apply { initializeDefaultValues(); oixEchTls = true; identity = false }
        assertFalse(parseSnell(editor.toUri()).identity)
        val cloned = editor.clone()
        assertFalse(cloned.identity)
        assertTrue(cloned.identityPresent)
        editor.identity = null
        assertEquals(true, buildSingBoxOutboundSnellBean(editor).oix_identity)
    }

    @Test fun yamlBooleanIdentityIsNotDropped() {
        val bean = parseClashSnell(source() + ("identity" to true))
        assertEquals(true, bean.identity)
        assertEquals(true, parseSnell(bean.toUri()).identity)
        assertEquals(true, buildSingBoxOutboundSnellBean(bean).oix_identity)
    }

    @Test fun uriPercentEncodingIsDecodedExactlyOnce() {
        val bean = parseClashSnell(source())
        val restored = parseSnell(bean.toUri())
        assertEquals(bean.psk, restored.psk)
        assertEquals(bean.name, restored.name)
        assertEquals(bean.oixConfig, restored.oixConfig)
        assertEquals(bean.serverAddress, restored.serverAddress)
    }

    @Test fun allYamlFieldsSurviveUriAndBeanAndReachOutbound() {
        val bean = parseClashSnell(source())
        val expected = mapOf("identity" to true, "oixPath" to "/路径?x=+&y=%25#z",
            "oixSkipCertVerify" to true, "tcpFastOpen" to true)
        val output = ByteBufferOutput(8192)
        bean.serialize(output)
        val stored = SnellBean().apply { deserialize(ByteBufferInput(output.toBytes())) }
        val linked = parseSnell(bean.toUri())
        for ((field, value) in expected) for (candidate in listOf(bean, stored, linked)) {
            assertEquals(field, value, candidate.javaClass.getField(field).get(candidate))
        }
        val outbound = buildSingBoxOutboundSnellBean(linked)
        for ((field, value) in mapOf("oix_identity" to expected["identity"], "oix_path" to expected["oixPath"],
            "oix_skip_cert_verify" to true, "tcp_fast_open" to true)) {
            assertEquals(field, value, outbound.javaClass.getField(field).get(outbound))
        }
        bean.oixEchTls = false
        assertNull(buildSingBoxOutboundSnellBean(bean).oix_ech)
        // Disabled Oix metadata is still retained for editing and sharing.
        val disabled = parseSnell(bean.toUri())
        assertFalse(disabled.oixEchTls)
        assertEquals(expected["oixPath"], disabled.javaClass.getField("oixPath").get(disabled))
    }
}
