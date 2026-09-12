package io.nekohasekai.sagernet.bg.proto

import io.nekohasekai.sagernet.bg.GuardedProcessPool
import io.nekohasekai.sagernet.database.ProxyEntity
import io.nekohasekai.sagernet.fmt.buildConfig
import io.nekohasekai.sagernet.ktx.Logs
import kotlinx.coroutines.*
import libcore.Libcore
import moe.matsuri.nb4a.net.LocalResolverImpl
import java.io.IOException
import java.util.concurrent.atomic.AtomicReference

class TestInstance(
    profile: ProxyEntity,
    val link: String,
    private val timeout: Int,
    preparedTunNetBatch: String? = null,
    tunNetSnapshotPath: String? = null,
) : BoxInstance(profile, preparedTunNetBatch, tunNetSnapshotPath) {

    // Structured ownership: cancellation waits for the existing native request timeout,
    // then closes this instance before the worker/batch can finish. Never close during init.
    suspend fun doTest(): Int = withContext(Dispatchers.IO) {
        val producerJob = currentCoroutineContext().job
        TemporaryLogProducers.register(producerJob)
        val fatal = AtomicReference<IOException?>()
        processes = GuardedProcessPool { fatal.compareAndSet(null, it) }
        val started = System.nanoTime()
        var prepared = started
        var launched = started
        var requested = started
        try {
            runOwnedUrlTest(
                prepare = { try { init() } finally { prepared = System.nanoTime() } },
                start = {
                    try {
                        launch()
                        if (processes.processCount > 0) delay(500)
                        fatal.get()?.let { throw it }
                    } finally { launched = System.nanoTime() }
                },
                request = {
                    try {
                        val result = Libcore.urlTest(box, link, timeout)
                        fatal.get()?.let { throw it }
                        result
                    } finally { requested = System.nanoTime() }
                },
                close = {
                    try { close() } finally {
                        processes.coroutineContext[Job]?.cancelAndJoin()
                        awaitLogWriters()
                    }
                },
            )
        } finally {
            TemporaryLogProducers.unregister(producerJob)
            Logs.i("URLTest phases prepareMs=${(prepared-started).coerceAtLeast(0)/1_000_000} startMs=${(launched-prepared).coerceAtLeast(0)/1_000_000} requestMs=${(requested-launched).coerceAtLeast(0)/1_000_000} totalMs=${(System.nanoTime()-started)/1_000_000}")
        }
    }

    override fun buildConfig() {
        config = buildConfig(
            profile,
            forTest = true,
            tunNetSnapshotPathProvider = tunNetSnapshotPath?.let { path -> { path } },
        )
    }

    override suspend fun loadConfig() {
        // Do not log config/credentials, or destroy unrelated JSI instances.
        box = Libcore.newSingBoxInstance(config.config, LocalResolverImpl)
    }
}
