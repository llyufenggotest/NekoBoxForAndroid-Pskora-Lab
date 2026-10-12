package io.nekohasekai.sagernet.fmt

import io.nekohasekai.sagernet.database.ProxyEntity
import io.nekohasekai.sagernet.fmt.xhttp.XHttpBean
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(manifest = Config.NONE, sdk = [28])
class BlackstoneUniversalFmtTest {
    @Test fun typeMapAcceptsLegacyAndCanonicalButSharesCanonical() {
        assertEquals(ProxyEntity.TYPE_XHTTP, TypeMap["blackstone"])
        assertEquals(ProxyEntity.TYPE_XHTTP, TypeMap["xhttp"])
        assertEquals("blackstone", TypeMap.reversed[ProxyEntity.TYPE_XHTTP])

        val bean = XHttpBean().apply {
            serverAddress = "node.example"
            serverPort = 443
            password = "token-fixture"
            nodeId = "node-fixture"
            deviceId = "device-fixture"
            deviceName = "tablet-fixture"
            initializeDefaultValues()
        }
        val link = bean.toUniversalLink()
        assertTrue(link.startsWith("sn://blackstone?"))
        assertFalse(link.startsWith("sn://null"))
        val restored = parseUniversal(link) as XHttpBean
        assertEquals("device-fixture", restored.deviceId)
        assertEquals("tablet-fixture", restored.deviceName)
    }
}
