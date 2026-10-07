package io.nekohasekai.sagernet.fmt

import android.app.Application
import com.google.gson.JsonParser
import io.nekohasekai.sagernet.*
import io.nekohasekai.sagernet.database.*
import io.nekohasekai.sagernet.fmt.socks.SOCKSBean
import io.nekohasekai.sagernet.fmt.internal.ChainBean
import org.junit.Assert.*
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [28], manifest = Config.NONE, application = Application::class)
class GroupDnsBuilderTest {
    @get:org.junit.Rule
    val roomLifecycle = io.nekohasekai.sagernet.RoomLifecycleRule()

    @Before fun setup() {
        DataStore.serviceMode = Key.MODE_PROXY
        DataStore.mixedPort = 2080
        DataStore.remoteDns = "https://1.1.1.1/dns-query"
        DataStore.directDns = "https://8.8.8.8/dns-query"
        DataStore.enableDnsRouting = true
        DataStore.enableTLSFragment = false
        SagerDatabase.proxyDao.reset(); SagerDatabase.groupDao.reset()
    }
    private fun group(id: Long, old: String?, newer: String? = null) = ProxyGroup(id = id,
        name = "synthetic-$id", customDirectDns = old,
        type = if (newer == null) GroupType.BASIC else GroupType.SUBSCRIPTION,
        subscription = newer?.let { value -> SubscriptionBean().apply { initializeDefaultValues(); serverDnsResolver = value } }
    ).also { SagerDatabase.groupDao.createGroup(it) }
    private fun node(id: Long, gid: Long, host: String = "shared.example") = ProxyEntity().apply {
        this.id = id; groupId = gid
        putBean(SOCKSBean().apply { initializeDefaultValues(); name = "node-$id"; serverAddress = host; serverPort = 1080 })
    }.also { SagerDatabase.proxyDao.addProxy(it) }
    private fun root(p: ProxyEntity) = JsonParser.parseString(buildConfig(p, forTest = true).config).asJsonObject.also { json ->
        System.getProperty("configbuild.evidenceDir")?.let { dir ->
            java.io.File(dir).apply { mkdirs() }.resolve("group-dns-${p.id}.json").writeText(json.toString())
        }
    }
    @Test fun subscriptionNewValueWinsAndBlankFallsBack() {
        group(101, "udp://192.0.2.1", "tcp://192.0.2.2:53")
        group(102, "udp://192.0.2.3", "   ")
        for ((gid, expected) in listOf(101L to "tcp://192.0.2.2:53", 102L to "udp://192.0.2.3")) {
            val json = root(node(gid, gid))
            val outbound = json.getAsJsonArray("outbounds").first().asJsonObject
            assertEquals("dns-sub-$gid", outbound.getAsJsonObject("domain_resolver")?.get("server")?.asString)
            assertEquals(expected, json.getAsJsonObject("dns").getAsJsonArray("servers").single {
                it.asJsonObject.get("tag").asString == "dns-sub-$gid"
            }.asJsonObject.get("address").asString)
        }
    }
    private fun externalNode(id: Long, gid: Long, bean: AbstractBean) = ProxyEntity().apply {
        this.id = id; groupId = gid
        bean.initializeDefaultValues(); bean.name = "external-$id"
        bean.serverAddress = "shared.example"; bean.serverPort = 443
        putBean(bean)
    }.also { SagerDatabase.proxyDao.addProxy(it) }

    @Test fun externalMappingResolvesWithLeafGroupAndPreservesChainDetour() {
        group(301, "udp://127.0.0.1:15301"); group(302, "tcp://127.0.0.1:15302")
        externalNode(311, 301, io.nekohasekai.sagernet.fmt.trojan_go.TrojanGoBean())
        externalNode(312, 302, io.nekohasekai.sagernet.fmt.naive.NaiveBean())
        val chain = ProxyEntity().apply { id = 313; groupId = 301; putBean(ChainBean().apply {
            initializeDefaultValues(); proxies = listOf(311L, 312L)
        }) }
        val json = root(chain)
        val outbounds = json.getAsJsonArray("outbounds").map { it.asJsonObject }
        val mappings = json.getAsJsonArray("inbounds").map { it.asJsonObject }.filter {
            it.get("override_address")?.asString == "shared.example"
        }
        assertEquals(2, mappings.size)
        for (mapping in mappings) {
            val id = mapping.get("tag").asString.substringAfterLast('-').toLong()
            val gid = if (id == 311L) 301 else 302
            val rules = json.getAsJsonObject("route").getAsJsonArray("rules").map { it.asJsonObject }.filter {
                it.getAsJsonArray("inbound")?.any { value -> value.asString == mapping.get("tag").asString } == true
            }
            val route = rules.single { it.has("outbound") }
            val target = outbounds.single { it.get("tag").asString == route.get("outbound").asString }
            if (id == 312L) {
                assertEquals("resolve", rules.first().get("action")?.asString)
                assertEquals("dns-sub-$gid", rules.first().get("server")?.asString)
                assertEquals("socks", target.get("type").asString)
            } else {
                assertEquals("direct", target.get("type").asString)
                assertEquals("dns-sub-$gid", target.getAsJsonObject("domain_resolver")?.get("server")?.asString)
                assertFalse(target.has("detour"))
            }
            val dnsServer = json.getAsJsonObject("dns").getAsJsonArray("servers").map { it.asJsonObject }.single {
                it.get("tag").asString == "dns-sub-$gid"
            }
            assertEquals(if (gid == 301) "udp://127.0.0.1:15301" else "tcp://127.0.0.1:15302",
                dnsServer.get("address").asString)
            assertEquals(TAG_DIRECT, dnsServer.get("detour").asString)
            val otherHop = outbounds.single { it.get("type").asString == "socks" &&
                it.get("tag").asString != json.getAsJsonObject("route").get("final").asString }
            // ChainBean reverses configured proxies: 312's mapping must still dial through 311.
            if (id == 312L) assertEquals(otherHop.get("tag").asString, target.get("tag").asString)
        }
        assertTrue(outbounds.filter { it.get("type").asString == "socks" }.all { !it.has("domain_resolver") })
        assertFalse(json.getAsJsonObject("dns").getAsJsonArray("rules").any {
            it.asJsonObject.getAsJsonArray("domain")?.any { host -> host.asString == "shared.example" } == true
        })
    }

    @Test fun externalFamiliesUseMappingAndUnsupportedMappingStaysExplicit() {
        group(401, "udp://127.0.0.1:15401")
        val beans = listOf(
            io.nekohasekai.sagernet.fmt.trojan_go.TrojanGoBean(),
            io.nekohasekai.sagernet.fmt.naive.NaiveBean(),
            io.nekohasekai.sagernet.fmt.mieru.MieruBean(),
            io.nekohasekai.sagernet.fmt.hysteria.HysteriaBean().apply {
                protocol = io.nekohasekai.sagernet.fmt.hysteria.HysteriaBean.PROTOCOL_WECHAT_VIDEO
            },
            io.nekohasekai.sagernet.fmt.hysteria.HysteriaBean().apply {
                protocol = io.nekohasekai.sagernet.fmt.hysteria.HysteriaBean.PROTOCOL_FAKETCP
            },
            moe.matsuri.nb4a.proxy.neko.NekoBean()
        )
        for ((index, bean) in beans.withIndex()) {
            val node = externalNode(411L + index, 401, bean)
            assertTrue(node.needExternal())
            val json = root(node)
            val maps = json.getAsJsonArray("inbounds").map { it.asJsonObject }.filter { it.has("override_address") }
            // A final-hop built-in Matsuri executable uses protected sockets,
            // deliberately not the mapping-inbound path. Keep that contract.
            val protectedHysteria = bean is io.nekohasekai.sagernet.fmt.hysteria.HysteriaBean &&
                moe.matsuri.nb4a.plugin.Plugins.isUsingMatsuriExe(
                    if (bean.protocolVersion == 1) "hysteria-plugin" else "hysteria2-plugin"
                )
            if (bean.canMapping() && !protectedHysteria) {
                assertEquals(bean.javaClass.simpleName, 1, maps.size)
                val rule = json.getAsJsonObject("route").getAsJsonArray("rules").map { it.asJsonObject }.single {
                    it.getAsJsonArray("inbound")?.any { value -> value.asString == maps.single().get("tag").asString } == true
                }
                val target = json.getAsJsonArray("outbounds").map { it.asJsonObject }.single {
                    it.get("tag").asString == rule.get("outbound").asString
                }
                assertEquals("direct", target.get("type").asString)
                assertEquals("dns-sub-401", target.getAsJsonObject("domain_resolver")?.get("server")?.asString)
                assertEquals("shared.example", maps.single().get("override_address").asString)
            } else {
                // FakeTCP and Neko explicitly opt out: plugin-owned DNS is outside mapping coverage.
                assertTrue(maps.isEmpty())
            }
            assertTrue(json.getAsJsonArray("outbounds").map { it.asJsonObject }.filter {
                it.get("type").asString == "socks"
            }.all { !it.has("domain_resolver") })
        }
    }

    @Test fun crossGroupChainUsesEachHopOwnerEvenWithSameHost() {
        group(201, "udp://192.0.2.1"); group(202, "udp://192.0.2.2")
        node(211, 201); node(212, 202, "SHARED.example.")
        val chain = ProxyEntity().apply { id = 213; groupId = 201; putBean(ChainBean().apply {
            initializeDefaultValues(); name = "synthetic-chain"; proxies = listOf(211L, 212L)
        }) }
        val json = root(chain)
        val hops = json.getAsJsonArray("outbounds").filter { it.asJsonObject.get("type").asString == "socks" }
        assertEquals(2, hops.size)
        for (hop in hops) {
            val out = hop.asJsonObject
            val gid = if (out.get("server").asString == "shared.example") 201 else 202
            assertEquals("dns-sub-$gid", out.getAsJsonObject("domain_resolver")?.get("server")?.asString)
        }
        assertFalse(json.getAsJsonObject("dns").getAsJsonArray("rules").any {
            it.asJsonObject.get("server")?.asString == "dns-airport"
        })
    }
}
