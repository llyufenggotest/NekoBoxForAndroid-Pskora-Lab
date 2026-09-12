package io.nekohasekai.sagernet.bg.proto

import kotlinx.coroutines.*
import org.junit.Assert.*
import org.junit.Test
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean

class UrlTestLifecycleTest {
    @Test fun cancelledBlockingRequestDrainsAndClosesWithoutPublishingFailure() = runBlocking {
        val entered = CountDownLatch(1); val release = CountDownLatch(1)
        val closed = AtomicBoolean(); val published = AtomicBoolean()
        val job = launch(Dispatchers.IO) {
            val result = runOwnedUrlTest(prepare = {}, start = {}, request = {
                entered.countDown(); check(release.await(5, TimeUnit.SECONDS)); 123
            }, close = { closed.set(true) })
            published.set(result == 123)
        }
        assertTrue(entered.await(5, TimeUnit.SECONDS))
        job.cancel(); delay(20)
        assertFalse(job.isCompleted); assertFalse(closed.get())
        release.countDown(); job.join()
        assertTrue(closed.get()); assertFalse(published.get())
    }
    @Test fun initFailureAndCancelDuringStartStillClose() = runBlocking {
        var closes = 0
        try {
            runOwnedUrlTest(prepare = { throw IllegalStateException("fixture") }, start = {}, request = { 1 }, close = { closes++ })
            fail("expected failure")
        } catch (_: IllegalStateException) { }
        assertEquals(1, closes)
        val entered = CompletableDeferred<Unit>()
        val job = launch {
            runOwnedUrlTest(prepare = {}, start = { entered.complete(Unit); awaitCancellation() }, request = { fail("must not request"); 1 }, close = { delay(10); closes++ })
        }
        entered.await(); job.cancelAndJoin(); assertEquals(2, closes)
    }
}
