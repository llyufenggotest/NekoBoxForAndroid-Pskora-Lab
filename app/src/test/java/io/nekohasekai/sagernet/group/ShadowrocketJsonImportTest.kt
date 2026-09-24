package io.nekohasekai.sagernet.group

import io.nekohasekai.sagernet.fmt.trojan.TrojanBean
import io.nekohasekai.sagernet.fmt.v2ray.VMessBean
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(manifest = Config.NONE, sdk = [28])
class ShadowrocketJsonImportTest {
    @Test fun shadowrocketVmessJsonImportsAsNativeBean() {
        val bean = RawUpdater.parseShadowrocketJson(JSONObject("""
            {"host":"103.242.3.46","type":"Vmess","method":"chacha20-poly1305",
             "port":"9527","alterId":"0","title":"Hong Kong 2",
             "password":"synthetic-uuid","udp":1,"obfs":"none"}
        """.trimIndent())) as VMessBean
        assertEquals("103.242.3.46", bean.serverAddress)
        assertEquals(9527, bean.serverPort)
        assertEquals("synthetic-uuid", bean.uuid)
        assertEquals("chacha20-poly1305", bean.encryption)
        assertEquals(0, bean.alterId)
        assertEquals("Hong Kong 2", bean.name)
    }

    @Test fun shadowrocketRealityVlessJsonImportsAsNativeBean() {
        val bean = RawUpdater.parseShadowrocketJson(JSONObject("""
            {"host":"jp.example","type":"VLESS","tls":true,"tlsProfile":"chrome",
             "port":"19012","peer":"www.example.com","password":"synthetic-uuid",
             "publicKey":"synthetic-public-key","shortId":"0123456789abcdef",
             "title":"JP-01","xtls":2,"obfs":"none"}
        """.trimIndent())) as VMessBean
        assertTrue(bean.isVLESS)
        assertEquals("tls", bean.security)
        assertEquals("www.example.com", bean.sni)
        assertEquals("chrome", bean.utlsFingerprint)
        assertEquals("synthetic-public-key", bean.realityPubKey)
        assertEquals("0123456789abcdef", bean.realityShortId)
    }

    @Test fun shadowrocketTrojanJsonImportsAsNativeBean() {
        val bean = RawUpdater.parseShadowrocketJson(JSONObject("""
            {"host":"trojan.example","type":"Trojan","port":"443","title":"Taiwan",
             "password":"synthetic-password","peer":"sni.example","tls":true}
        """.trimIndent())) as TrojanBean
        assertEquals("trojan.example", bean.serverAddress)
        assertEquals(443, bean.serverPort)
        assertEquals("synthetic-password", bean.password)
        assertEquals("Taiwan", bean.name)
        assertEquals("sni.example", bean.sni)
    }

    @Test fun singBoxTrojanJsonIsNotMisclassifiedAsShadowrocket() {
        val json = JSONObject("""
            {"type":"trojan","server":"node.example","server_port":443,
             "password":"synthetic","tls":{"enabled":true,"server_name":"sni.example"}}
        """.trimIndent())
        assertEquals(null, RawUpdater.parseShadowrocketJson(json))
    }
}
