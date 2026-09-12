package io.nekohasekai.sagernet.bg.proto

import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.joinAll

/** UI-process temporary producers; no changes to batch scheduling outside reload. */
internal object TemporaryLogProducers {
    private val jobs = mutableSetOf<Job>()
    private var paused = false
    @Synchronized fun pause() { paused = true }
    @Synchronized fun resume() { paused = false }
    @Synchronized fun register(job: Job) {
        if (paused) throw CancellationException("Log configuration is changing")
        jobs.add(job)
    }
    @Synchronized fun unregister(job: Job) { jobs.remove(job) }
    suspend fun quiesce() {
        val snapshot = synchronized(this) { jobs.toList() }
        snapshot.forEach { it.cancel(CancellationException("Log configuration is changing")) }
        snapshot.joinAll()
    }
}
