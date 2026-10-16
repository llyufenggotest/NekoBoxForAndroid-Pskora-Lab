package io.nekohasekai.sagernet.fmt.v2ray

import okhttp3.HttpUrl.Companion.toHttpUrl
import org.junit.Assert.*
import org.junit.Test

class BlackstoneVlessLinkTest {
    @Test fun shareLinkRetainsExactSelectorAndOpaquePrivateFields() {
        for (e in listOf(false, true)) {
            val bean = VMessBean().apply {
                alterId = -1; uuid = "00112233-4455-6677-8899-aabbccddeeff#blackstone"
                serverAddress = "node.example"; serverPort = 443
                blackstoneE = e; blackstoneH = "03aabbcc"; initializeDefaultValues()
            }
            val url = bean.toUriVMessVLESSTrojan(false).replace("vless://", "https://").toHttpUrl()
            val restored = VMessBean().apply { alterId = -1; parseDuckSoft(url); initializeDefaultValues() }
            assertEquals(bean.uuid, restored.uuid)
            assertEquals(e, restored.blackstoneE); assertEquals(bean.blackstoneH, restored.blackstoneH)
        }
    }

    @Test fun ordinaryShareLinkDoesNotAcquirePrivateParameters() {
        val bean = VMessBean().apply { alterId = -1; initializeDefaultValues() }
        val url = bean.toUriVMessVLESSTrojan(false).replace("vless://", "https://").toHttpUrl()
        assertNull(url.queryParameter("e")); assertNull(url.queryParameter("h"))
    }

    @Test fun invalidBooleanQueryIsNotSilentlyCoerced() {
        assertTrue(runCatching {
            VMessBean().apply {
                alterId = -1
                parseDuckSoft("https://00112233-4455-6677-8899-aabbccddeeff%23blackstone@node.example:443?e=1&h=03aabbcc".toHttpUrl())
            }
        }.isFailure)
    }
}
