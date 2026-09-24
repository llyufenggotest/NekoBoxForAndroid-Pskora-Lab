package io.nekohasekai.sagernet.ui

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class PreferredTestResultsPriorityTest {
    @Test fun pendingManualRejectsOverlappingAndLateOldAutomaticBatch() {
        var now = 1_000L
        val results = PreferredTestResults(clock = { now }, manualPriorityMillis = { 300_000L })
        results.replaceAutomatic("session", mapOf(7L to PreferredAutoSample(900L, 80)))
        val ticket = results.begin(7L, "URLTest")
        assertEquals(null, results.label(7L))
        assertEquals("64 ms", results.numericLabel(7L, persistedStatus = 1, persistedPing = 64))

        // A batch begun before manual completion may arrive later; it must not win.
        results.replaceAutomatic("session", mapOf(7L to PreferredAutoSample(950L, 30)))
        now = 1_100L
        results.complete(7L, ticket, 1, 42)
        assertEquals("42 ms", results.label(7L))
        results.replaceAutomatic("session", mapOf(7L to PreferredAutoSample(1_050L, 25)))
        assertEquals("42 ms", results.label(7L))
    }

    @Test fun failedManualRetainsLastSuccessfulPersistedLatency() {
        val results = PreferredTestResults(clock = { 1_000L })
        val ticket = results.begin(9L, "URLTest")
        results.complete(9L, ticket, status = 3, ping = 0)
        assertEquals("88 ms", results.numericLabel(9L, persistedStatus = 1, persistedPing = 88))
    }

    @Test fun stalePendingTicketCannotClearNewerAttempt() {
        val results = PreferredTestResults(clock = { 1_000L })
        val oldTicket = results.begin(5L, "URLTest")
        val newTicket = results.begin(5L, "TCPing")
        assertEquals(false, results.finishPending(5L, oldTicket))
        assertEquals(true, results.finishPending(5L, newTicket))
    }

    @Test fun newerPeriodicAutomaticTakesOverAfterOneInterval() {
        var now = 10_000L
        var interval = 300_000L
        val results = PreferredTestResults(clock = { now }, manualPriorityMillis = { interval })
        val ticket = results.begin(7L, "TCPing")
        results.complete(7L, ticket, 1, 42)

        now += interval - 1
        results.replaceAutomatic("session", mapOf(7L to PreferredAutoSample(now, 20)))
        assertEquals("42 ms", results.label(7L))

        now += 1
        results.replaceAutomatic("session", mapOf(7L to PreferredAutoSample(now, 19)))
        assertEquals("19 ms", results.label(7L))
    }

    @Test fun aLaterIntervalChangeAppliesToTheNextManualCompletion() {
        var now = 20_000L
        var interval = 300_000L
        val results = PreferredTestResults(clock = { now }, manualPriorityMillis = { interval })
        val first = results.begin(3L, "URLTest")
        results.complete(3L, first, 2, 0)
        interval = 60_000L
        now += 1_000L
        val second = results.begin(3L, "URLTest")
        results.complete(3L, second, 2, 0)
        now += 59_999L
        results.replaceAutomatic("session", mapOf(3L to PreferredAutoSample(now, 11)))
        assertEquals(null, results.label(3L))
        now += 1L
        results.replaceAutomatic("session", mapOf(3L to PreferredAutoSample(now, 10)))
        assertEquals("10 ms", results.label(3L))
    }

    @Test fun disconnectedAutomaticSnapshotDoesNotInventAResult() {
        val results = PreferredTestResults(clock = { 1_000L }, manualPriorityMillis = { 300_000L })
        results.replaceAutomatic("", mapOf(1L to PreferredAutoSample(900L, 7)))
        assertNull(results.label(1L))
    }
}
