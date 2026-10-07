package io.nekohasekai.sagernet.fmt

import android.app.Application
import com.google.gson.JsonObject
import com.google.gson.JsonParser
import io.nekohasekai.sagernet.Key
import io.nekohasekai.sagernet.database.DataStore
import io.nekohasekai.sagernet.database.ProxyEntity
import io.nekohasekai.sagernet.fmt.snell.SnellBean
import io.nekohasekai.sagernet.fmt.socks.SOCKSBean
import org.junit.Assert.*
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import java.io.File

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [28], manifest = Config.NONE, application = Application::class)
class OixManagedDnsTest {
    @get:org.junit.Rule
    val roomLifecycle = io.nekohasekai.sagernet.RoomLifecycleRule()

    @Before fun setup() {
        DataStore.serviceMode = Key.MODE_PROXY
        DataStore.mixedPort = 2080
        DataStore.remoteDns = "https://1.1.1.1/dns-query"
        DataStore.directDns = "https://8.8.8.8/dns-query"
        DataStore.enableDnsRouting = true
    }

    private fun snell(host: String, oix: Boolean) = SnellBean().apply {
        initializeDefaultValues()
        serverAddress = host
        serverPort = 443
        psk = "synthetic-key"
        oixEchTls = oix
        if (oix) {
            oixSni = "synthetic.example"
            oixConfig = "AQID" // Synthetic non-empty value; startup does not perform a TLS handshake.
        }
    }

    private fun generated(bean: AbstractBean, forTest: Boolean = true): JsonObject {
        val proxy = ProxyEntity().apply { id = 919; groupId = 0; putBean(bean) }
        val text = buildConfig(proxy, forTest = forTest, forExport = !forTest).config
        System.getProperty("configbuild.evidenceDir")?.let {
            File(it).mkdirs()
            File(it, "oix-managed-${bean.javaClass.simpleName}-${bean.serverAddress}-${bean is SnellBean && bean.oixEchTls}-$forTest.json").writeText(text)
        }
        return JsonParser.parseString(text).asJsonObject
    }

    private fun assertDedicated(root: JsonObject, forTest: Boolean = true) {
        val outbound = root.getAsJsonArray("outbounds").first { it.asJsonObject.get("type").asString == "snell" }.asJsonObject
        assertEquals("dns-oix-managed", outbound.getAsJsonObject("domain_resolver").get("server").asString)
        val dns = root.getAsJsonObject("dns")
        val server = dns.getAsJsonArray("servers").single { it.asJsonObject.get("tag").asString == "dns-oix-managed" }.asJsonObject
        assertEquals("tcp://124.221.68.73:1053", server.get("address").asString)
        assertEquals(TAG_DIRECT, server.get("detour").asString)
        assertFalse(server.has("address_resolver"))
        assertEquals(if (forTest) "dns-direct" else "dns-remote", dns.get("final").asString)
        assertFalse(dns.getAsJsonArray("rules").any { it.asJsonObject.get("server")?.asString == "dns-oix-managed" })
        if (!forTest) assertEquals("https://1.1.1.1/dns-query", dns.getAsJsonArray("servers").single { it.asJsonObject.get("tag").asString == "dns-remote" }.asJsonObject.get("address").asString)
        assertEquals("https://8.8.8.8/dns-query", dns.getAsJsonArray("servers").single { it.asJsonObject.get("tag").asString == "dns-direct" }.asJsonObject.get("address").asString)
        val direct = root.getAsJsonArray("outbounds").single { it.asJsonObject.get("tag").asString == TAG_DIRECT }.asJsonObject
        assertEquals("direct", direct.get("type").asString)
        assertFalse(direct.has("detour"))
    }

    @Test fun realBuildConfigUsesDirectManagedResolverAndPreservesStandardNodes() {
        // URLTest path avoids Android-only java.net.Socket reflection on host JVM.
        val managed = generated(snell("synthetic.cloud-nodes.com", true))
        assertDedicated(managed)
        val ordinaryDns = generated(snell("synthetic.cloud-nodes.com", false)).getAsJsonObject("dns")
        val managedDns = managed.getAsJsonObject("dns").deepCopy()
        managedDns.getAsJsonArray("servers").remove(managedDns.getAsJsonArray("servers").single {
            it.asJsonObject.get("tag").asString == "dns-oix-managed"
        })
        assertEquals("Global DNS unchanged except independent server", ordinaryDns, managedDns)
        dnsNamesAreCaseInsensitiveAndRootedNamesAreSupported()
        ordinarySnellOtherOixDomainsAndOtherProtocolsKeepDns()
    }

    private fun dnsNamesAreCaseInsensitiveAndRootedNamesAreSupported() {
        for (host in listOf("cloud-nodes.com", "Synthetic.CLOUD-NODES.COM.")) assertDedicated(generated(snell(host, true)))
    }

    private fun ordinarySnellOtherOixDomainsAndOtherProtocolsKeepDns() {
        val socks = SOCKSBean().apply { initializeDefaultValues(); serverAddress = "synthetic.cloud-nodes.com"; serverPort = 1080 }
        for (bean in listOf(snell("synthetic.cloud-nodes.com", false), snell("other.example", true),
            snell("evilcloud-nodes.com", true), snell("cloud-nodes.com.evil.example", true), snell("127.0.0.1", true), socks)) {
            val root = generated(bean)
            assertFalse(root.getAsJsonObject("dns").getAsJsonArray("servers").any { it.asJsonObject.get("tag").asString == "dns-oix-managed" })
            assertFalse(root.getAsJsonArray("outbounds").any { it.asJsonObject.has("domain_resolver") })
        }
    }
}
