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

    @Test fun oixUsesGroupResolverAndNeverBuiltInFallback() {
        val bean = snell("synthetic.cloud-nodes.com", true)
        val owner = io.nekohasekai.sagernet.database.ProxyGroup(
            id = 701, name = "synthetic-group", customDirectDns = " tcp://192.0.2.71:1053 "
        )
        assertEquals("tcp://192.0.2.71:1053", serverDnsResolverFor(bean, owner))
        io.nekohasekai.sagernet.database.SagerDatabase.groupDao.createGroup(owner)
        val proxy = ProxyEntity().apply { id = 920; groupId = owner.id; putBean(bean) }
        val root = JsonParser.parseString(buildConfig(proxy, forTest = true).config).asJsonObject
        val outbound = root.getAsJsonArray("outbounds").first { it.asJsonObject.get("type").asString == "snell" }.asJsonObject
        assertEquals("dns-sub-701", outbound.getAsJsonObject("domain_resolver").get("server").asString)
        val dns = root.getAsJsonObject("dns")
        assertEquals("tcp://192.0.2.71:1053", dns.getAsJsonArray("servers").single {
            it.asJsonObject.get("tag").asString == "dns-sub-701"
        }.asJsonObject.get("address").asString)
        assertFalse(dns.getAsJsonArray("rules").any { it.asJsonObject.get("server")?.asString == "dns-sub-701" })
        assertFalse(root.toString().contains("dns-oix-managed"))
        assertFalse(root.toString().contains("124.221.68.73"))
        owner.customDirectDns = " \n\t "
        assertNull(serverDnsResolverFor(bean, owner))
        assertNull(serverDnsResolverFor(bean, null))
        io.nekohasekai.sagernet.database.SagerDatabase.groupDao.updateGroup(owner)
        val blank = JsonParser.parseString(buildConfig(proxy, forTest = true).config).asJsonObject
        assertFalse(blank.getAsJsonArray("outbounds").any { it.asJsonObject.has("domain_resolver") })
        assertFalse(blank.toString().contains("dns-sub-701"))
    }

    @Test fun blankGroupDnsPreservesNormalDnsAndStandardNodes() {
        val managed = generated(snell("synthetic.cloud-nodes.com", true))
        assertFalse(managed.toString().contains("124.221.68.73"))
        assertEquals(generated(snell("synthetic.cloud-nodes.com", false)).getAsJsonObject("dns"), managed.getAsJsonObject("dns"))
        for (host in listOf("cloud-nodes.com", "Synthetic.CLOUD-NODES.COM.")) {
            val bean = snell(host, true)
            assertTrue(usesOixManagedDns(bean))
            assertNull(serverDnsResolverFor(bean, null))
            assertFalse(generated(bean).getAsJsonArray("outbounds").any { it.asJsonObject.has("domain_resolver") })
        }
        ordinarySnellOtherOixDomainsAndOtherProtocolsKeepDns()
    }

    @Test fun subscriptionPrecedenceAppliesToOixWithOnlyLegacyFieldFallback() {
        val bean = snell("synthetic.cloud-nodes.com", true)
        val subscription = io.nekohasekai.sagernet.database.SubscriptionBean().apply {
            initializeDefaultValues(); serverDnsResolver = " tcp://192.0.2.72:53 "
        }
        val owner = io.nekohasekai.sagernet.database.ProxyGroup(
            id = 702, name = "subscription", type = io.nekohasekai.sagernet.GroupType.SUBSCRIPTION,
            customDirectDns = "udp://192.0.2.73:53", subscription = subscription
        )
        assertEquals("tcp://192.0.2.72:53", serverDnsResolverFor(bean, owner))
        subscription.serverDnsResolver = " \n "
        assertEquals("udp://192.0.2.73:53", serverDnsResolverFor(bean, owner))
        owner.customDirectDns = " "
        assertNull(serverDnsResolverFor(bean, owner))
        for (ordinary in listOf(snell("other.example", true), snell("synthetic.cloud-nodes.com", false),
            snell("cloud-nodes.com.evil.example", true), snell("evilcloud-nodes.com", true),
            snell("synthetic.cloud-nodes.com", true).apply { version = 6 })) {
            assertFalse(usesOixManagedDns(ordinary))
        }
    }

    @Test fun managedSignedNamesAreNeverPreResolvedOrRewritten() {
        val updater = object : io.nekohasekai.sagernet.group.GroupUpdater() {
            override suspend fun doUpdate(
                proxyGroup: io.nekohasekai.sagernet.database.ProxyGroup,
                subscription: io.nekohasekai.sagernet.database.SubscriptionBean,
                userInterface: io.nekohasekai.sagernet.database.GroupManager.Interface?,
                byUser: Boolean
            ) {}
            fun resolve(bean: AbstractBean) = kotlinx.coroutines.runBlocking {
                forceResolve(listOf(bean), null)
            }
            fun rewrite(bean: AbstractBean) = rewriteAddress(
                bean, listOf(java.net.InetAddress.getByAddress(byteArrayOf(127, 0, 0, 1))), false
            )
        }
        val bean = snell("Synthetic.CLOUD-NODES.COM.", true)
        updater.resolve(bean)
        updater.rewrite(bean)
        assertEquals("Synthetic.CLOUD-NODES.COM.", bean.serverAddress)
        val ordinary = snell("ordinary.example", false)
        updater.rewrite(ordinary)
        assertEquals("127.0.0.1", ordinary.serverAddress)
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
