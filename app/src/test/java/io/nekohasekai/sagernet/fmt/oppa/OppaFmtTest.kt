package io.nekohasekai.sagernet.fmt.oppa

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.Base64

class OppaFmtTest {

    @Test
    fun parseOfficialProviderLink() {
        val json = """{"name":"Test Provider","apiToken":"${"a".repeat(32)}","apiEndPoint":"provider.example","encryptionKey":"${"b".repeat(32)}"}"""
        val encoded = Base64.getUrlEncoder().withoutPadding().encodeToString(json.toByteArray())

        val provider = parseOppaProvider("oppa://$encoded")

        assertEquals("Test Provider", provider.name)
        assertEquals("provider.example", provider.apiEndpoint)
        assertEquals(32, provider.apiToken.length)
        assertEquals(32, provider.encryptionKey.length)
    }

    @Test
    fun nodeLinkRoundTripDoesNotLeakProviderSecrets() {
        val bean = OppaBean().apply {
            serverAddress = "node.example"
            serverPort = 443
            password = "p".repeat(32)
            preConnect = 12
            sni = "tls.example"
            allowInsecure = false
            certificatePinSHA256 = "synthetic-pin"
            name = "Oppa Test"
        }

        val link = bean.toUri()
        val parsed = parseOppaNode(link)

        assertEquals(bean.serverAddress, parsed.serverAddress)
        assertEquals(bean.serverPort, parsed.serverPort)
        assertEquals(bean.password, parsed.password)
        assertEquals(bean.preConnect, parsed.preConnect)
        assertEquals(bean.sni, parsed.sni)
        assertEquals(bean.allowInsecure, parsed.allowInsecure)
        assertEquals(bean.certificatePinSHA256, parsed.certificatePinSHA256)
        assertEquals(bean.name, parsed.name)
        assertFalse(link.contains("apiToken"))
        assertFalse(link.contains("encryptionKey"))
    }

    @Test
    fun ipv6NodeLinkRoundTrip() {
        val bean = OppaBean().apply {
            serverAddress = "2001:db8::1"
            serverPort = 8443
            password = "z".repeat(32)
            preConnect = 8
            name = "IPv6"
        }

        val link = bean.toUri()
        assertTrue(link.contains("[2001:db8::1]:8443"))
        assertEquals("2001:db8::1", parseOppaNode(link).serverAddress)
    }

    @Test
    fun percentEncodedFieldsRoundTrip() {
        val bean = OppaBean().apply {
            serverAddress = "node.example"
            serverPort = 443
            password = "future@token+with space"
            sni = "tls.example"
            name = "中文 Oppa"
        }
        val parsed = parseOppaNode(bean.toUri())
        assertEquals(bean.password, parsed.password)
        assertEquals(bean.name, parsed.name)
        assertEquals(bean.sni, parsed.sni)
    }

    @Test
    fun variableLengthNodePasswordIsPreserved() {
        val parsed = parseOppaNode("oppa://future-token@node.example:443#future")
        assertEquals("future-token", parsed.password)
    }

    @Test(expected = IllegalArgumentException::class)
    fun rejectEmptyNodePassword() {
        parseOppaNode("oppa://@node.example:443#bad")
    }
}
