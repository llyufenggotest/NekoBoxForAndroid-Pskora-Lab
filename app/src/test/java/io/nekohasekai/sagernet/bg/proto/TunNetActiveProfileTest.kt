package io.nekohasekai.sagernet.bg.proto

import io.nekohasekai.sagernet.database.ProxyEntity
import io.nekohasekai.sagernet.fmt.v2ray.VMessBean
import io.nekohasekai.sagernet.fmt.v2ray.isTunNet
import io.nekohasekai.sagernet.fmt.v2ray.tunNetSelection
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class TunNetActiveProfileTest {

    private fun entity(id: Long, uuid: String, address: String = "seed.example") =
        ProxyEntity(id = id).putBean(VMessBean().apply {
            alterId = -1
            this.uuid = uuid
            serverAddress = address
            serverPort = 443
            initializeDefaultValues()
        })

    @Test
    fun usesOfficialTunNetControlVersionMetadata() {
        assertEquals("0.2.6", TUN_NET_CLIENT_VERSION)
    }

    @Test
    fun tunNetSelectorTransitionsRequireRestart() {
        assertTrue(selectorSwitchRequiresRestart(currentTunNet = true, nextTunNet = false))
        assertTrue(selectorSwitchRequiresRestart(currentTunNet = false, nextTunNet = true))
        assertTrue(selectorSwitchRequiresRestart(currentTunNet = true, nextTunNet = true))
        assertFalse(selectorSwitchRequiresRestart(currentTunNet = false, nextTunNet = false))
    }

    @Test
    fun selectsOnlyCurrentProfileChainFromSelectorTrafficMap() {
        val first = entity(1, "123e4567-e89b-82d3-a456-426614174000#TunNet:5Y2X5L-h5o6l5YWl54K5:jp-01")
        val second = entity(2, "123e4567-e89b-82d3-a456-426614174000#TunNet:56e75Yqo5o6l5YWl54K5:us-04")
        val trafficMap = mapOf("first" to listOf(first), "second" to listOf(second))
        val active = activeTunNetBeans(1, mapOf(1L to "first", 2L to "second"), trafficMap)

        assertEquals(1, active.size)
        assertEquals("jp-01", active.single().tunNetSelection()!!.hostSlug)
    }

    @Test
    fun includesTunNetInsideCurrentChainOnly() {
        val ordinary = entity(3, "123e4567-e89b-82d3-a456-426614174000")
        val tunNet = entity(4, "123e4567-e89b-82d3-a456-426614174000#TunNet")
        val inactive = entity(5, "123e4567-e89b-82d3-a456-426614174000#TunNet:56e75Yqo5o6l5YWl54K5:sg-01")
        val active = activeTunNetBeans(
            3,
            mapOf(3L to "chain", 5L to "inactive"),
            mapOf("chain" to listOf(ordinary, tunNet), "inactive" to listOf(inactive)),
        )

        assertEquals(1, active.size)
        assertTrue(active.single().isTunNet())
    }
}
