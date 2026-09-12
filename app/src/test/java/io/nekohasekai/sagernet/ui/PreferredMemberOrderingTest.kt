package io.nekohasekai.sagernet.ui

import org.junit.Assert.assertEquals
import org.junit.Test

class PreferredMemberOrderingTest {
    private data class Member(val id: Long, val persistedStatus: Int = 0, val persistedPing: Int = 0)

    @Test fun healthyMembersSortByLatencyAndTiesKeepReferenceOrder() {
        val members = listOf(Member(1, 1, 80), Member(2, 1, 20), Member(3, 1, 20))
        assertEquals(listOf(2L, 3L, 1L), preferredMembersByLatency(members,
            id = { it.id }, persistedStatus = { it.persistedStatus }, persistedPing = { it.persistedPing },
            results = PreferredTestResults()).map { it.id })
    }

    @Test fun pendingUntestedAndFailedStayBehindHealthyInReferenceOrder() {
        val results = PreferredTestResults(clock = { 1_000L })
        results.begin(2L, "URLTest")
        val members = listOf(Member(1, 0), Member(2, 1, 1), Member(3, 2), Member(4, 1, 50))
        assertEquals(listOf(4L, 1L, 2L, 3L), preferredMembersByLatency(members,
            id = { it.id }, persistedStatus = { it.persistedStatus }, persistedPing = { it.persistedPing },
            results = results).map { it.id })
    }

    @Test fun eachManualAndAutomaticResultCanReorderWithoutChangingIdentity() {
        var now = 1_000L
        val results = PreferredTestResults(clock = { now }, manualPriorityMillis = { 1L })
        val members = listOf(Member(11), Member(22))
        fun order() = preferredMembersByLatency(members, { it.id }, { it.persistedStatus }, { it.persistedPing }, results)
            .map { it.id }

        assertEquals(listOf(11L, 22L), order())
        results.replaceAutomatic("running", mapOf(22L to PreferredAutoSample(900L, 40)))
        assertEquals(listOf(22L, 11L), order())
        val ticket = results.begin(11L, "URLTest")
        assertEquals(listOf(22L, 11L), order())
        now = 1_100L
        results.complete(11L, ticket, 1, 10)
        assertEquals(listOf(11L, 22L), order())
    }
}
