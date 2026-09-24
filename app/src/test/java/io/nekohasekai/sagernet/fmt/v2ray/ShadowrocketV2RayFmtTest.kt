package io.nekohasekai.sagernet.fmt.v2ray

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(manifest = Config.NONE, sdk = [28])
class ShadowrocketV2RayFmtTest {
    @Test fun shadowrocketVlessWebSocketLinkMapsHostSniAndName() {
        val bean = parseV2Ray(
            "vless://bm9uZTpzeW50aGV0aWMtdXVpZEBub2RlLmV4YW1wbGU6NDQz" +
                "?path=/nekocloud/hk-01&remarks=HK-Test&obfsParam=ws.example.com" +
                "&obfs=websocket&tls=1&peer=tls.example.com&tfo=1"
        ) as VMessBean

        assertTrue(bean.isVLESS)
        assertEquals("synthetic-uuid", bean.uuid)
        assertEquals("node.example", bean.serverAddress)
        assertEquals(443, bean.serverPort)
        assertEquals("HK-Test", bean.name)
        assertEquals("ws", bean.type)
        assertEquals("/nekocloud/hk-01", bean.path)
        assertEquals("ws.example.com", bean.host)
        assertEquals("tls", bean.security)
        assertEquals("tls.example.com", bean.sni)
    }

    @Test fun shadowrocketRealityVlessAliasesMapToRealityFields() {
        val bean = parseV2Ray(
            "vless://bm9uZTpzeW50aGV0aWMtdXVpZEBqcC5leGFtcGxlOjE5MDEy" +
                "?remarks=JP-01&tls=1&peer=www.example.com&udp=1&xtls=2&alterId=0" +
                "&pbk=synthetic-public-key&sid=0123456789abcdef&fingerprint=chrome"
        ) as VMessBean

        assertTrue(bean.isVLESS)
        assertEquals("tls", bean.security)
        assertEquals("www.example.com", bean.sni)
        assertEquals("synthetic-public-key", bean.realityPubKey)
        assertEquals("0123456789abcdef", bean.realityShortId)
        assertEquals("chrome", bean.utlsFingerprint)
        assertTrue(bean.type == "tcp" || bean.type.isEmpty())
        assertEquals(-1, bean.alterId)
    }

    @Test fun kitsunebiJsonObfsParamStillExtractsHost() {
        val bean = parseV2Ray(
            "vmess://Y2hhY2hhMjAtcG9seTEzMDU6c3ludGhldGljLXV1aWRAbm9kZS5leGFtcGxlOjQ0Mw" +
                "?remarks=JSON-Host&obfs=websocket&obfsParam=%7B%22Host%22%3A%22ws.example.com%22%7D"
        ) as VMessBean
        assertEquals("ws.example.com", bean.host)
    }

    @Test fun shadowrocketVmessLegacyBase64AuthorityStillParses() {
        val bean = parseV2Ray(
            "vmess://Y2hhY2hhMjAtcG9seTEzMDU6c3ludGhldGljLXV1aWRAMTAzLjI0Mi4zLjQ2Ojk1Mjc" +
                "?remarks=HongKong&udp=1&alterId=0"
        ) as VMessBean

        assertEquals("chacha20-poly1305", bean.encryption)
        assertEquals("synthetic-uuid", bean.uuid)
        assertEquals("103.242.3.46", bean.serverAddress)
        assertEquals(9527, bean.serverPort)
        assertEquals(0, bean.alterId)
        assertEquals("HongKong", bean.name)
    }
}
