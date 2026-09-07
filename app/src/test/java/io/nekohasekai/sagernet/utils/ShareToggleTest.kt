package io.nekohasekai.sagernet.utils

import org.junit.Assert.*
import org.junit.Test

class ShareToggleTest {
    @Test fun allFourInitialPreferenceCombinations() {
        for (shared in listOf(false, true)) for (api in listOf(false, true)) {
            var storedShare = shared
            var storedApi = api
            var reloads = 0
            var writes = 0
            assertTrue(ShareToggle.apply(shared, !shared, true, {
                storedShare = it; storedApi = it; writes++
            }, { assertEquals(storedShare, storedApi); reloads++ }))
            assertEquals(!shared, storedShare)
            assertEquals(!shared, storedApi)
            assertEquals(1, writes)
            assertEquals(1, reloads)
        }
    }
    @Test fun restoredAndRepeatedStateDoesNotOverrideManualApi() {
        for (shared in listOf(false, true)) for (api in listOf(false, true)) {
            var storedApi = api
            assertFalse(ShareToggle.apply(shared, shared, true, { storedApi = it }, { fail("unexpected reload") }))
            assertEquals(api, storedApi)
        }
    }
    @Test fun closeThenManualApiRemainsIndependent() {
        var shared = true
        var api = true
        var reloads = 0
        val write: (Boolean) -> Unit = { shared = it; api = it }
        ShareToggle.apply(shared, false, true, write, { reloads++ })
        assertFalse(api)
        api = true // independent API switch after closing sharing
        ShareToggle.apply(shared, false, true, write, { reloads++ })
        assertTrue(api)
        api = false
        ShareToggle.apply(shared, false, true, write, { reloads++ })
        assertFalse(api)
        assertEquals(1, reloads)
    }
    @Test fun stoppedToggleWritesWithoutReloadAndCanBeReversed() {
        var shared = false
        var api = true
        var writes = 0
        for (requested in listOf(true, false, true, false)) {
            ShareToggle.apply(shared, requested, false, { shared = it; api = it; writes++ }, { fail("stopped") })
            assertEquals(requested, shared)
            assertEquals(requested, api)
        }
        assertEquals(4, writes)
    }
}
