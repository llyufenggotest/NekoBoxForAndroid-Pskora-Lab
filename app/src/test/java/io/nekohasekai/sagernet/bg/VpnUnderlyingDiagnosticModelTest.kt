package io.nekohasekai.sagernet.bg

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class VpnUnderlyingDiagnosticModelTest {
    @Test fun aToBToARecordsOrderedRequestedHashes() {
        val model = VpnUnderlyingDiagnosticModel()
        val lines = listOf(11L, 22L, 11L).map { model.recordSetRequest(it, VpnUnderlyingDiagnosticModel.Result.TRUE, 7L) }
        assertEquals(listOf(1L, 2L, 3L), lines.map { it.sequence })
        assertEquals(listOf(11L, 22L, 11L), lines.map { it.requestedHash })
        assertTrue(lines.all { it.requested && !it.offline && it.resultCode == 1 })
    }

    @Test fun bAToBToAHasOneFrameworkWriteAndThreeCoreCallbacks() {
        val model = VpnUnderlyingDiagnosticModel()
        val initial = model.recordSetRequest(11L, VpnUnderlyingDiagnosticModel.Result.TRUE, 7L)
        val selected = listOf(11L, 22L, 11L)
        val callbacks = selected.map { model.recordCallback(99L, it, true, true, true, 1, it, 7L) }
        val suppressed = selected.drop(1).map { model.recordSuppressedEstablishedUpdate(it, 7L) }
        assertEquals(1, listOf(initial).count { it.requested })
        assertEquals(3, callbacks.size)
        assertTrue(suppressed.all { !it.requested && it.kind == 3 })
        assertEquals(listOf(11L, 22L, 11L), callbacks.map { it.selectedHash })
    }
    @Test fun sSuppressionIsDistinctAndKeepsRetainedUnderlying() {
        val model = VpnUnderlyingDiagnosticModel()
        model.recordSetRequest(11L, VpnUnderlyingDiagnosticModel.Result.TRUE, 7L)
        val suppressed = model.recordSuppressedEstablishedUpdate(22L, 7L)
        assertEquals(3, suppressed.kind)
        assertFalse(suppressed.requested)
        assertEquals(22L, suppressed.requestedHash)
        assertEquals(11L, suppressed.retainedHash)
        assertEquals(4, suppressed.resultCode)
    }

    @Test fun iInitialOmissionAndLiveSuppressionNeverClaimFrameworkRequests() {
        val model = VpnUnderlyingDiagnosticModel()
        val initial = model.recordInitialOmitted(11L, 7L)
        val live = model.recordSuppressedEstablishedUpdate(22L, 7L)
        assertEquals(4, initial.kind)
        assertFalse(initial.requested)
        assertEquals(11L, initial.requestedHash)
        assertEquals(0L, initial.retainedHash)
        assertEquals(3, live.kind)
        assertFalse(live.requested)
        assertEquals(0L, live.retainedHash)
        assertTrue(initial.render().contains("kind=4 requested=0"))
        assertTrue(live.render().contains("kind=3 requested=0"))
    }
    @Test fun offlineDoesNotPretendCurrentCodeClearedUnderlying() {
        val model = VpnUnderlyingDiagnosticModel()
        model.recordSetRequest(11L, VpnUnderlyingDiagnosticModel.Result.TRUE, 7L)
        val offline = model.recordOfflineNoRequest(7L)
        assertFalse(offline.requested)
        assertTrue(offline.offline)
        assertEquals(0, offline.argumentCode)
        assertEquals(11L, offline.retainedHash)
    }

    @Test fun falseAndExceptionAreDifferentResultClasses() {
        val model = VpnUnderlyingDiagnosticModel()
        val rejected = model.recordSetRequest(11L, VpnUnderlyingDiagnosticModel.Result.FALSE, 7L)
        val failed = model.recordSetRequest(22L, VpnUnderlyingDiagnosticModel.Result.EXCEPTION, 9L, exceptionClass = 3)
        assertEquals(2, rejected.resultCode)
        assertEquals(3, failed.resultCode)
        assertEquals(3, failed.exceptionClass)
    }

    @Test fun duplicateCallbacksRemainVisibleWithoutInventingWrites() {
        val model = VpnUnderlyingDiagnosticModel()
        val first = model.recordCallback(11L, 11L, true, true, true, 1)
        val duplicate = model.recordCallback(11L, 11L, true, true, true, 3)
        assertFalse(first.duplicate)
        assertTrue(duplicate.duplicate)
        assertEquals(2L, duplicate.sequence)
        assertEquals(3, duplicate.callbackCode)
    }

    @Test fun renderedSchemaContainsOnlyNumericBooleansAndThreadHash() {
        val line = VpnUnderlyingDiagnosticModel().recordSetRequest(11L, VpnUnderlyingDiagnosticModel.Result.FALSE, 7L).render()
        assertTrue(line.startsWith("VPNUNDERTRACE schema=2 "))
        assertTrue(line.contains("requested=1"))
        assertTrue(line.contains("result=2"))
        assertTrue(line.contains("thread=7"))
        assertFalse(line.contains("false"))
    }
}
