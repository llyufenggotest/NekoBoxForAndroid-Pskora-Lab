package io.nekohasekai.sagernet.bg.proto

import io.nekohasekai.sagernet.database.ProxyEntity
import io.nekohasekai.sagernet.database.ProxyGroup
import io.nekohasekai.sagernet.fmt.v2ray.TunNetSelection
import org.junit.Assert.*
import org.junit.Test

class UrlTestPreparationTest {
    @Test fun complexUnknownAndGroupDetoursAlwaysUseFullParser() {
        for (type in listOf(ProxyEntity.TYPE_CHAIN, ProxyEntity.TYPE_CONFIG, ProxyEntity.TYPE_OPPA, ProxyEntity.TYPE_NEKO, 123456)) {
            var calls = 0
            val expected = TunNetSelection("entry", "host")
            assertEquals(expected, discoverUrlTestTunNet(ProxyEntity(type = type), ProxyGroup()) { calls++; expected })
            assertEquals(1, calls)
        }
        for (group in listOf(null, ProxyGroup(frontProxy = 42), ProxyGroup(landingProxy = 43))) {
            var calls = 0
            discoverUrlTestTunNet(ProxyEntity(type = ProxyEntity.TYPE_SS), group) { calls++; null }
            assertEquals(1, calls)
        }
    }
    @Test fun ordinaryNodeDoesNotBuildConfigurationForDiscovery() {
        var builds = 0
        val result = discoverUrlTestTunNet(ProxyEntity().apply { type = ProxyEntity.TYPE_SS }, ProxyGroup()) {
            builds++
            null
        }
        assertNull(result)
        assertEquals(0, builds)
    }
}
