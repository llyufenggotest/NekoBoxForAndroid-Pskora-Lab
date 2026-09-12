package moe.matsuri.nb4a.proxy.config

import org.junit.Assert.*
import org.junit.Test

class PreferredGroupSpecTest {
    private fun rejects(block: () -> Unit) = assertThrows(IllegalArgumentException::class.java, block)

    @Test fun acceptsExplicitAndDynamicSourcesAtInclusiveBounds() {
        for (interval in listOf(1, 86400)) for (delay in listOf(0, 65535)) {
            assertEquals(listOf(1L), PreferredGroupSpec(listOf(1), listOf(2), interval, delay)
                .validate(setOf(1), setOf(2), 3))
            PreferredGroupSpec(sourceGroupIds = listOf(2), intervalSeconds = interval,
                minDelayMilliseconds = delay).validate(emptySet(), setOf(2), 3)
        }
    }

    @Test fun rejectsEmptyMissingNonPositiveDuplicateAndSelfSources() {
        val invalid = listOf(PreferredGroupSpec(), PreferredGroupSpec(listOf(9)),
            PreferredGroupSpec(listOf(0)), PreferredGroupSpec(listOf(-1)), PreferredGroupSpec(listOf(1, 1)),
            PreferredGroupSpec(sourceGroupIds = listOf(9)), PreferredGroupSpec(sourceGroupIds = listOf(0)),
            PreferredGroupSpec(sourceGroupIds = listOf(2, 2)), PreferredGroupSpec(sourceGroupIds = listOf(3)))
        invalid.forEach { spec -> rejects { spec.validate(setOf(-1, 0, 1), setOf(0, 2, 3), 3) } }
    }

    @Test fun rejectsOutOfRangeTiming() {
        for (interval in listOf(-1, 0, 86401)) rejects {
            PreferredGroupSpec(listOf(1), intervalSeconds = interval).validate(setOf(1), emptySet())
        }
        for (delay in listOf(-1, 65536)) rejects {
            PreferredGroupSpec(listOf(1), minDelayMilliseconds = delay).validate(setOf(1), emptySet())
        }
    }

    @Test fun guardDetectsCycleAndUnwindsAfterException() {
        val guard = PreferredReferenceGuard()
        val error = rejects { guard.visit(1) { guard.visit(2) { guard.visit(1) {} } } }
        assertTrue(error.message!!.contains("1 → 2 → 1"))
        assertEquals(7, guard.visit(1) { 7 })
        assertThrows(IllegalStateException::class.java) { guard.visit(1) { error("candidate failed") } }
        guard.visit(1) { guard.visit(2) {}; guard.visit(2) {} }
    }

    @Test fun depthBoundaryAllows64AndRejects65WithoutPoisoningGuard() {
        val guard = PreferredReferenceGuard()
        fun descend(n: Long): Unit = guard.visit(n) { if (n > 1) descend(n - 1) }
        descend(64)
        rejects { descend(65) }
        descend(64)
    }

    @Test fun modeValidationAndNativeCapabilityGateAreBehavioral() {
        for (mode in listOf(PREFERRED_MODE_LATENCY, PREFERRED_MODE_STABLE))
            PreferredGroupSpec(listOf(1), mode = mode).validate(setOf(1), emptySet())
        rejects { PreferredGroupSpec(listOf(1), mode = "invalid").validate(setOf(1), emptySet()) }
        rejects { nativeUrlTest("p", listOf("a"), 60, 0, PREFERRED_MODE_STABLE, false) }
        rejects { nativeUrlTest("p", listOf("a"), 60, 0, "invalid", true) }
        for (mode in listOf(PREFERRED_MODE_LATENCY, PREFERRED_MODE_STABLE)) {
            val outbound = nativeUrlTest("p", listOf("a"), 60, 0, mode, true)
            assertEquals(true, outbound["dial_fallback"])
            assertEquals(mode, outbound["fallback_mode"])
        }
        val legacyCore = nativeUrlTest("p", listOf("a"), 60, 0, PREFERRED_MODE_LATENCY, false)
        assertFalse(legacyCore.containsKey("dial_fallback"))
        assertFalse(legacyCore.containsKey("fallback_mode"))
    }

    @Test fun nativeOutboundHasOrderedUniqueMembersAndExplicitZeroTolerance() {
        val release = nativeUrlTest("preferred", listOf("b", "a", "b"), 60, 0)
        assertEquals(true, release["dial_fallback"])
        assertEquals(PREFERRED_MODE_LATENCY, release["fallback_mode"])
        val result = nativeUrlTest("preferred", listOf("b", "a", "b"), 60, 0, verifiedDialFallback = false)
        assertEquals(mapOf("type" to "urltest", "tag" to "preferred", "outbounds" to listOf("b", "a"),
            "interval" to "60s", "tolerance" to 0), result)
        for (members in listOf(emptyList(), listOf(""), listOf("preferred")))
            rejects { nativeUrlTest("preferred", members, 60, 0) }
    }
}
