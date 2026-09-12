package io.nekohasekai.sagernet.bg.proto

import kotlinx.coroutines.*
import org.junit.Assert.*
import org.junit.Test
import java.util.concurrent.atomic.AtomicInteger

class UrlTestBatchTest {
    @Test fun selectionDoesNotTreatFailuresAsUntested() {
        assertTrue(UrlTestScope.UNTESTED.includes(0))
        for (s in listOf(-1, 1, 2, 3)) assertFalse(UrlTestScope.UNTESTED.includes(s))
        assertTrue(UrlTestScope.FAILED.includes(2))
        assertTrue(UrlTestScope.FAILED.includes(3))
        for (s in listOf(-1, 0, 1)) assertFalse(UrlTestScope.FAILED.includes(s))
    }
    @Test fun concurrencyBounds() {
        assertEquals(1, urlTestWorkerCount(0, 20))
        assertEquals(16, urlTestWorkerCount(100, 20))
        assertEquals(5, urlTestWorkerCount(5, 20))
        assertEquals(0, urlTestWorkerCount(5, 0))
    }
    @Test fun exactlyOnceAndWaitsForResources() = runBlocking {
        val active = AtomicInteger(); val peak = AtomicInteger(); val seen = java.util.concurrent.ConcurrentHashMap.newKeySet<Int>()
        runUrlTestBatch((1..40).toList(), 5) { id ->
            val n = active.incrementAndGet(); peak.updateAndGet { maxOf(it, n) }
            try { delay(5); assertTrue(seen.add(id)) } finally { active.decrementAndGet() }
        }
        assertEquals(40, seen.size); assertEquals(0, active.get()); assertTrue(peak.get() <= 5)
    }
    @Test fun cancelDrainsInFlightBeforeReturningAndNeverStartsQueueTail() = runBlocking {
        val entered = CompletableDeferred<Unit>(); val drained = AtomicInteger(); val started = AtomicInteger()
        val job = launch {
            runUrlTestBatch((1..100).toList(), 1) {
                started.incrementAndGet(); entered.complete(Unit)
                try { awaitCancellation() } finally { withContext(NonCancellable) { delay(30); drained.incrementAndGet() } }
            }
        }
        entered.await(); job.cancelAndJoin()
        assertEquals(1, drained.get()); assertEquals(1, started.get())
    }
}
