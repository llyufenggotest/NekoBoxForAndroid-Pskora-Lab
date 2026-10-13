package io.nekohasekai.sagernet.group

import io.nekohasekai.sagernet.fmt.shadowsocks.ShadowsocksBean
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test

class GostPluginYamlImportTest {
    private fun yaml(pluginOpts: String) = """
        proxies:
          - name: gost fixture
            type: ss
            server: node.invalid
            port: 443
            cipher: aes-256-gcm
            password: test-only-password
            plugin: gost-plugin
            plugin-opts:
${pluginOpts.prependIndent("              ")}
    """.trimIndent()

    private fun parse(pluginOpts: String): ShadowsocksBean = runBlocking {
        RawUpdater.parseRaw(yaml(pluginOpts), "fixture.yaml").orEmpty().single() as ShadowsocksBean
    }

    @Test fun typedMapIsCanonicalAndPreservesUnknownFields() {
        val bean = parse("""
            mode: websocket
            host: cdn.invalid
            path: /gost
            tls: true
            mux: 3
            nocomp: true
            fingerprint: chrome
            future-option: value;with=separators
        """.trimIndent())
        assertEquals(
            "gost-plugin;mode=mwss;host=cdn.invalid;path=/gost;nocomp;mux=3;fingerprint=chrome;future-option=value\\;with\\=separators",
            bean.plugin,
        )
    }

    @Test fun stringAndMapFormsProduceTheSameCanonicalPlugin() {
        val fromMap = parse("""
            mode: websocket
            host: cdn.invalid
            path: /gost
            nocomp: true
        """.trimIndent())
        val fromString = parse("'mode=ws;path=/gost;nocomp;host=cdn.invalid'")
        assertEquals("gost-plugin;mode=ws;host=cdn.invalid;path=/gost;nocomp", fromMap.plugin)
        assertEquals(fromMap.plugin, fromString.plugin)
    }

    @Test fun missingNullAndEmptyOptionsDoNotCrash() = runBlocking {
        val documents = listOf(
            yaml("{}"),
            yaml("null"),
            yaml("''"),
            """
                proxies:
                  - name: gost fixture
                    type: ss
                    server: node.invalid
                    port: 443
                    cipher: aes-256-gcm
                    password: test-only-password
                    plugin: gost-plugin
            """.trimIndent(),
        )
        documents.forEach { source ->
            val bean = RawUpdater.parseRaw(source).orEmpty().single() as ShadowsocksBean
            assertEquals("gost-plugin", bean.plugin)
        }
    }

    @Test fun unknownBooleansKeepExplicitTrueAndFalseValues() {
        val bean = parse("""
            mode: grpc
            experimental-true: true
            experimental-false: false
        """.trimIndent())
        assertEquals(
            "gost-plugin;mode=grpc;experimental-true=true;experimental-false=false",
            bean.plugin,
        )
    }

    @Test fun nestedUnknownValuesAreRejectedWithTheirFieldName() {
        val error = assertThrows(IllegalArgumentException::class.java) {
            parse("""
                mode: ws
                advanced:
                  retries: 3
            """.trimIndent())
        }
        assertTrue(error.message.orEmpty().contains("advanced"))
    }

    @Test fun ordinaryPluginsKeepTheirExistingSerialization() {
        val obfs = runBlocking {
            RawUpdater.parseRaw(yaml("{mode: http, host: cdn.invalid}").replace("gost-plugin", "obfs"))
                .orEmpty().single() as ShadowsocksBean
        }
        val v2ray = runBlocking {
            RawUpdater.parseRaw(yaml("{mode: websocket, host: cdn.invalid, path: /v, mux: true}")
                .replace("gost-plugin", "v2ray-plugin")).orEmpty().single() as ShadowsocksBean
        }
        assertEquals("obfs-local;obfs=http;obfs-host=cdn.invalid", obfs.plugin)
        assertEquals("v2ray-plugin;mode=websocket;host=cdn.invalid;path=/v;mux=8", v2ray.plugin)
        assertTrue(obfs.password.startsWith("test-only-"))
    }
}
