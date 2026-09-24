package io.nekohasekai.sagernet.fmt.v2ray

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ShadowrocketV2RayFmtTest {
    @Test fun shadowrocketVlessWebSocketPayloadAndAliasesDecode() {
        val parsed = parseShadowrocketLegacyLink(
            "vless://bm9uZTpzeW50aGV0aWMtdXVpZEBub2RlLmV4YW1wbGU6NDQz" +
                "?path=/nekocloud/hk-01&remarks=HK-Test&obfsParam=ws.example.com" +
                "&obfs=websocket&tls=1&peer=tls.example.com&tfo=1"
        )
        assertTrue(parsed.isVless)
        assertEquals("synthetic-uuid", parsed.uuid)
        assertEquals("node.example", parsed.server)
        assertEquals(443, parsed.port)
        assertEquals("HK-Test", parsed.query["remarks"])
        assertEquals("ws.example.com", parsed.query["obfsParam"])
        assertEquals("tls.example.com", parsed.query["peer"])
    }

    @Test fun shadowrocketRealityVlessPreservesSentinelInputs() {
        val parsed = parseShadowrocketLegacyLink(
            "vless://bm9uZTpzeW50aGV0aWMtdXVpZEBqcC5leGFtcGxlOjE5MDEy" +
                "?remarks=JP-01&tls=1&peer=www.example.com&alterId=0" +
                "&pbk=synthetic-public-key&sid=0123456789abcdef&fingerprint=chrome"
        )
        assertTrue(parsed.isVless)
        assertEquals("synthetic-public-key", parsed.query["pbk"])
        assertEquals("0123456789abcdef", parsed.query["sid"])
        assertEquals("chrome", parsed.query["fingerprint"])
        assertEquals("0", parsed.query["alterId"])
    }

    @Test fun shadowrocketVmessLegacyBase64AuthorityDecodes() {
        val parsed = parseShadowrocketLegacyLink(
            "vmess://Y2hhY2hhMjAtcG9seTEzMDU6c3ludGhldGljLXV1aWRAMTAzLjI0Mi4zLjQ2Ojk1Mjc" +
                "?remarks=HongKong&udp=1&alterId=0"
        )
        assertFalse(parsed.isVless)
        assertEquals("chacha20-poly1305", parsed.encryption)
        assertEquals("synthetic-uuid", parsed.uuid)
        assertEquals("103.242.3.46", parsed.server)
        assertEquals(9527, parsed.port)
    }

    @Test fun jsonObfsParamIsUrlDecodedForHostExtraction() {
        val parsed = parseShadowrocketLegacyLink(
            "vmess://Y2hhY2hhMjAtcG9seTEzMDU6c3ludGhldGljLXV1aWRAbm9kZS5leGFtcGxlOjQ0Mw" +
                "?obfs=websocket&obfsParam=%7B%22Host%22%3A%22ws.example.com%22%7D"
        )
        assertEquals("{\"Host\":\"ws.example.com\"}", parsed.query["obfsParam"])
    }
}
