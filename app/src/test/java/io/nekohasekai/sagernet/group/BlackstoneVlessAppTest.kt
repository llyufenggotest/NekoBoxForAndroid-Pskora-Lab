package io.nekohasekai.sagernet.group

import android.app.Application
import com.google.gson.JsonParser
import io.nekohasekai.sagernet.Key
import io.nekohasekai.sagernet.RoomLifecycleRule
import io.nekohasekai.sagernet.database.DataStore
import io.nekohasekai.sagernet.database.ProxyEntity
import io.nekohasekai.sagernet.database.ProxyGroup
import io.nekohasekai.sagernet.database.SagerDatabase
import io.nekohasekai.sagernet.fmt.KryoConverters
import io.nekohasekai.sagernet.fmt.buildConfig
import io.nekohasekai.sagernet.fmt.v2ray.VMessBean
import kotlinx.coroutines.runBlocking
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [28], manifest = Config.NONE, application = Application::class)
class BlackstoneVlessAppTest {
    @get:org.junit.Rule val roomLifecycle = RoomLifecycleRule()

    private fun yaml(extra: String = "", identity: String = "$UUID#blackstone", type: String = "vless") = """
        proxies:
          - name: synthetic-blackstone
            type: $type
            server: node.example
            port: 443
            uuid: '$identity'
            e: true
            h: '03aabbcc'
        ${extra.prependIndent("    ")}
    """.trimIndent()

    private fun imported(raw: String) = runBlocking {
        RawUpdater.parseRaw(raw).orEmpty().single() as VMessBean
    }

    private fun config(bean: VMessBean): com.google.gson.JsonObject {
        DataStore.serviceMode = Key.MODE_PROXY
        DataStore.mixedPort = 2080
        DataStore.remoteDns = "https://1.1.1.1/dns-query"
        DataStore.directDns = "https://8.8.8.8/dns-query"
        DataStore.enableDnsRouting = true
        DataStore.enableTLSFragment = false
        val group = ProxyGroup(name = "synthetic").also { it.id = SagerDatabase.groupDao.createGroup(it) }
        val entity = ProxyEntity().apply { groupId = group.id; putBean(bean) }
        entity.id = SagerDatabase.proxyDao.addProxy(entity)
        val persisted = SagerDatabase.proxyDao.getById(entity.id)!!
        return JsonParser.parseString(buildConfig(persisted, forTest = true).config).asJsonObject
            .getAsJsonArray("outbounds").first { it.asJsonObject.get("type")?.asString == "vless" }.asJsonObject
    }

    @Test fun yamlToConfigPreservesTypedFields() {
        val out = config(imported(yaml()))
        assertEquals("$UUID#blackstone", out.get("uuid").asString)
        assertEquals("private e must reach ConfigBuilder", true, out.get("e")?.asBoolean)
        assertEquals("03aabbcc", out.get("h")?.asString)
        System.getProperty("blackstone.fixture.path")?.let {
            java.io.File(it).writeText(out.toString())
        }
    }

    @Test fun blobAndCloneRetainPrivateFieldsBeforeConfig() {
        val bean = imported(yaml())
        val stored = KryoConverters.vmessDeserialize(KryoConverters.serialize(bean))
        val out = config(stored.clone())
        assertEquals(true, out.get("e")?.asBoolean)
        assertEquals("03aabbcc", out.get("h")?.asString)
    }

    @Test fun explicitFalseRemainsBooleanNotString() {
        val out = config(imported(yaml().replace("e: true", "e: false")))
        assertEquals(false, out.get("e")?.asBoolean)
        assertTrue(out.getAsJsonPrimitive("e").isBoolean)
    }

    @Test fun privateFieldsRejectInvalidYamlTypesWithoutCoercion() {
        for ((from, to) in listOf("e: true" to "e: 'true'", "e: true" to "e: 1",
            "h: '03aabbcc'" to "h: 123", "h: '03aabbcc'" to "h: [bad]")) {
            assertTrue("Invalid private type must fail", runCatching { imported(yaml().replace(from, to)) }.isFailure)
        }
    }

    @Test fun unrelatedVlessDoesNotAcquirePrivateFields() {
        for (id in listOf(UUID, "$UUID#x365", "$UUID#juzi")) {
            // Ordinary profiles without e/h must retain their existing JSON shape.
            val raw = yaml(identity = id).lineSequence().filterNot {
                it.trimStart().startsWith("e:") || it.trimStart().startsWith("h:")
            }.joinToString("\n")
            val out = config(imported(raw))
            assertEquals(id, out.get("uuid").asString)
            assertFalse(out.has("e")); assertFalse(out.has("h"))
        }
    }

    companion object { const val UUID = "12345678-1234-1234-1234-123456789abc" }
}
