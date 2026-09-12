package moe.matsuri.nb4a

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class PhysicalDefaultNetworkSelectorTest {
    @Test fun activeDefaultWinsLateCallbacksForTwentyRounds() {
        val a = 101L
        val b = 202L
        val vpn = 303L
        repeat(20) {
            assertEquals(a, PhysicalDefaultNetworkSelector.select(a, b, emptyList()) { it == a || it == b })
            assertEquals(b, PhysicalDefaultNetworkSelector.select(b, a, emptyList()) { it == a || it == b })
            assertEquals(a, PhysicalDefaultNetworkSelector.select(a, b, emptyList()) { it == a || it == b })
            assertEquals(a, PhysicalDefaultNetworkSelector.select(vpn, b, listOf(a)) { it == a || it == b })
            assertEquals(b, PhysicalDefaultNetworkSelector.select(null, b, emptyList()) { it == a || it == b })
            assertNull(PhysicalDefaultNetworkSelector.select(vpn, b, emptyList()) { false })
        }
    }
}
