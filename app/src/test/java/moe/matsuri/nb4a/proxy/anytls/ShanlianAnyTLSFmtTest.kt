package moe.matsuri.nb4a.proxy.anytls

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ShanlianAnyTLSFmtTest {
    private val credential = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

    @Test
    fun suffixLivesOnPasswordAndIsPreservedForRuntime() {
        val bean = AnyTLSBean().apply {
            password = "$credential#sl"
            serverAddress = "exit.example"
            serverPort = 443
            sni = "v-thumb.byteimg.com"
            initializeDefaultValues()
        }
        assertTrue(bean.isShanlian())
        assertEquals("$credential#sl", buildSingBoxOutboundAnyTLSBean(bean).password)
    }

    @Test
    fun suffixIsCaseInsensitiveAndShareLinkRoundTrips() {
        val bean = AnyTLSBean().apply {
            password = "$credential#SL"
            serverAddress = "exit.example"
            serverPort = 443
            sni = "v-thumb.byteimg.com"
            initializeDefaultValues()
        }
        val restored = parseAnytls(bean.toUri())
        assertTrue(restored.isShanlian())
        assertEquals(bean.password, restored.password)
    }

    @Test
    fun ordinaryAnyTLSAndSimilarSuffixStayStandard() {
        for (password in listOf("ordinary", "$credential#sl-extra")) {
            val bean = AnyTLSBean().apply { this.password = password; initializeDefaultValues() }
            assertFalse(bean.isShanlian())
            assertEquals(password, buildSingBoxOutboundAnyTLSBean(bean).password)
        }
        val malformed = AnyTLSBean().apply { password = "short#sl"; initializeDefaultValues() }
        assertFalse(malformed.isShanlian())
    }
}
