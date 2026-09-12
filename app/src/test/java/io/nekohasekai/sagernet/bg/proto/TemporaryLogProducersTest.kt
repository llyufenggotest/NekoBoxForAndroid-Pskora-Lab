package io.nekohasekai.sagernet.bg.proto

import kotlinx.coroutines.*
import org.junit.Assert.*
import org.junit.Test

class TemporaryLogProducersTest {
    @Test fun reloadWaitsForOldProducerCleanupAndRejectsNewWork() = runBlocking {
        TemporaryLogProducers.resume()
        var closed = false
        val started = CompletableDeferred<Unit>()
        val producer = launch {
            val owner = currentCoroutineContext().job
            TemporaryLogProducers.register(owner)
            try { started.complete(Unit); awaitCancellation() }
            finally {
                withContext(NonCancellable) { delay(10); closed = true }
                TemporaryLogProducers.unregister(owner)
            }
        }
        started.await()
        TemporaryLogProducers.pause()
        try {
            val rejected = Job()
            try { TemporaryLogProducers.register(rejected); fail("new producer accepted") }
            catch (_: CancellationException) { }
            finally { rejected.cancel() }
            TemporaryLogProducers.quiesce()
            assertTrue(closed)
            assertTrue(producer.isCompleted)
        } finally { TemporaryLogProducers.resume() }
    }
}
