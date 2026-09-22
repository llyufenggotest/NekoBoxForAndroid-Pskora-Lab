package io.nekohasekai.sagernet.group

import kotlinx.coroutines.runBlocking
import moe.matsuri.nb4a.proxy.anytls.AnyTLSBean
import moe.matsuri.nb4a.proxy.anytls.isShanlian
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class ShanlianAnyTLSYamlImportTest {
    private val password = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f#sl"

    @Test
    fun clipboardClashYamlImportsAllShanlianFields() = runBlocking {
        val yaml = """
            mixed-port: 7890
            proxies:
              - name: 香港闪连
                type: anytls
                server: 183.179.43.253
                port: 443
                password: '$password'
                sni: v-thumb.byteimg.com
                client-fingerprint: chrome
                skip-cert-verify: true
                alpn: [h2, http/1.1]
                min-idle-session: 2
                idle-session-check-interval: 30
                idle-session-timeout: 60
        """.trimIndent()
        val beans = RawUpdater.parseRaw(yaml).orEmpty()
        assertEquals(1, beans.size)
        val bean = beans.single() as AnyTLSBean
        assertEquals("香港闪连", bean.name)
        assertEquals("183.179.43.253", bean.serverAddress)
        assertEquals(443, bean.serverPort)
        assertEquals(password, bean.password)
        assertEquals("v-thumb.byteimg.com", bean.sni)
        assertEquals("chrome", bean.utlsFingerprint)
        assertTrue(bean.allowInsecure)
        assertEquals("h2\nhttp/1.1", bean.alpn)
        assertEquals(2, bean.minIdleSession)
        assertEquals(30, bean.idleSessionCheckInterval)
        assertEquals(60, bean.idleSessionTimeout)
        assertTrue(bean.isShanlian())
    }

    @Test
    fun yamlAliasesAndCaseInsensitiveTypeAreAccepted() = runBlocking {
        val yaml = """
            "proxies" :
              - name: HK alias
                type: AnyTLS
                server: hk.example
                server_port: 8443
                password: '$password'
                server_name: hk-sni.example
                fingerprint: chrome
                insecure: 1
                min_idle_session: 1
                idle_session_check_interval: 20s
                idle_session_timeout: 45s
        """.trimIndent()
        val bean = RawUpdater.parseRaw(yaml).orEmpty().single() as AnyTLSBean
        assertEquals(8443, bean.serverPort)
        assertEquals("hk-sni.example", bean.sni)
        assertEquals("chrome", bean.utlsFingerprint)
        assertTrue(bean.allowInsecure)
        assertEquals(1, bean.minIdleSession)
        assertEquals(20, bean.idleSessionCheckInterval)
        assertEquals(45, bean.idleSessionTimeout)
        assertTrue(bean.isShanlian())
    }
}
