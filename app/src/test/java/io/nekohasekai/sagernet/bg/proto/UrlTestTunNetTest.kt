package io.nekohasekai.sagernet.bg.proto

import io.nekohasekai.sagernet.database.ProxyEntity
import io.nekohasekai.sagernet.database.ProxyGroup
import io.nekohasekai.sagernet.fmt.v2ray.VMessBean
import io.nekohasekai.sagernet.fmt.v2ray.TunNetSelection
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [28], manifest = Config.NONE)
class UrlTestTunNetTest {
    private fun node(uuid: String) = ProxyEntity().apply {
        putBean(VMessBean().apply { initializeDefaultValues(); alterId = -1; this.uuid = uuid })
    }
    @Test fun fixedTunNetDecodesIndependentlyWithoutConfigurationOrIdentityCache() {
        val a = node("credential-a#tunnet:ZW50cnktYQ:host-a")
        val b = node("credential-b#tunnet:ZW50cnktYg:host-b")
        assertEquals(TunNetSelection("entry-a", "host-a"), discoverUrlTestTunNet(a, ProxyGroup()) { fail("unexpected parse"); null })
        assertEquals(TunNetSelection("entry-b", "host-b"), discoverUrlTestTunNet(b, ProxyGroup()) { fail("unexpected parse"); null })
    }
    @Test fun dynamicAndMalformedSelectionRemainOnLegacyInstanceSyncPath() {
        for (uuid in listOf("credential#tunnet", "credential#tunnet:invalid:!", "ordinary")) {
            assertNull(discoverUrlTestTunNet(node(uuid), ProxyGroup()) { fail("unexpected parse"); null })
        }
    }
    @Test fun tunNetGroupDetourDoesNotBypassFullValidation() {
        var parsed = false
        discoverUrlTestTunNet(node("credential#tunnet:ZW50cnk:host"), ProxyGroup(frontProxy = 1)) { parsed = true; null }
        assertTrue(parsed)
    }
}
