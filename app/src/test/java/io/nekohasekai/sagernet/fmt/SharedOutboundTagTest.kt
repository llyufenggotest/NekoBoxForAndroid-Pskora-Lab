package io.nekohasekai.sagernet.fmt

import org.junit.Assert.*
import org.junit.Test

class SharedOutboundTagTest {
    @Test fun sharedTerminalUsesExistingReadableTagBeforeDependencyIsEmitted() {
        val globals = mapOf(1L to "Standard SOCKS")
        val tag = resolveSharedOutboundTag("g-1", true, 1, globals)
        assertEquals("Standard SOCKS", tag)
        assertNotEquals("g-1", tag)
    }

    @Test fun uncachedTerminalKeepsProposedGlobalTag() {
        assertEquals("g-2", resolveSharedOutboundTag("g-2", true, 2, mapOf(1L to "Standard SOCKS")))
    }

    @Test fun nonTerminalHopDoesNotReuseGlobalAndChangeItsDetour() {
        assertEquals("chain-1", resolveSharedOutboundTag("chain-1", false, 1, mapOf(1L to "Standard SOCKS")))
    }

    @Test fun duplicateSingleNodeUsesExistingTagWithoutChangingCache() {
        val globals = mutableMapOf(1L to "g-1")
        assertEquals("g-1", resolveSharedOutboundTag("Readable duplicate", true, 1, globals))
        assertEquals(mapOf(1L to "g-1"), globals)
    }
}
