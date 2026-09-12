package io.nekohasekai.sagernet.fmt

import com.google.gson.Gson
import com.google.gson.JsonParser
import org.junit.Assert.*
import org.junit.Test
import java.io.File

class CachePolicyTest {
    @Test fun generatedTestAndVpnCachePolicy() {
        for (forTest in listOf(true, false)) {
            val json = Gson().toJson(buildExperimentalOptions(forTest, true))
            val obj = JsonParser.parseString(json).asJsonObject
            assertEquals(!forTest, obj["cache_file"].asJsonObject["enabled"].asBoolean)
            if (forTest) {
                assertFalse(obj.has("clash_api"))
                assertFalse(obj["cache_file"].asJsonObject.has("path"))
            } else {
                assertEquals("../cache/cache.db", obj["cache_file"].asJsonObject["path"].asString)
                assertTrue(obj["cache_file"].asJsonObject["store_fakeip"].asBoolean)
                assertTrue(obj.has("clash_api"))
            }
            val dir = File("../evidence/concurrency-routing/generated").apply { mkdirs() }
            File(dir, if(forTest) "test-experimental.json" else "vpn-experimental.json").writeText(json)
        }
    }
    @Test fun actualBuildConfigRawProfileWritesTestOverrideAndPreservesVpn() {
        val raw = """{"log":{"level":"info"},"outbounds":[{"type":"direct","tag":"proxy"}]}"""
        val bean = moe.matsuri.nb4a.proxy.config.ConfigBean().apply { type = 0; name = "cache-probe"; config = raw }
        val profile = io.nekohasekai.sagernet.database.ProxyEntity().putBean(bean)
        assertEquals(raw, buildConfig(profile, forTest = false).config)
        val generated = buildConfig(profile, forTest = true).config
        assertFalse(JsonParser.parseString(generated).asJsonObject["experimental"].asJsonObject["cache_file"].asJsonObject["enabled"].asBoolean)
        val dir = File("../evidence/concurrency-routing/generated").apply { mkdirs() }
        File(dir, "actual-buildConfig-test.json").writeText(generated)
    }
    @Test fun rawConfigCacheOverridePreservesOtherFields() {
        val raw = """{"log":{"level":"debug"},"experimental":{"cache_file":{"enabled":true,"path":"vpn.db"},"clash_api":{"secret":"keep"}}}"""
        val result = JsonParser.parseString(disableTestCache(raw)).asJsonObject
        assertFalse(result["experimental"].asJsonObject["cache_file"].asJsonObject["enabled"].asBoolean)
        assertEquals("keep", result["experimental"].asJsonObject["clash_api"].asJsonObject["secret"].asString)
        assertEquals("debug", result["log"].asJsonObject["level"].asString)
    }
    @Test fun configBuilderUsesTestFlagWithoutGuardAndKeepsLogging() {
        val source = File("src/main/java/io/nekohasekai/sagernet/fmt/ConfigBuilder.kt").readText()
        assertTrue(source.contains("experimental = buildExperimentalOptions(forTest, DataStore.enableClashAPI)"))
        assertTrue(source.contains("log = LogOptions().apply"))
    }
}
