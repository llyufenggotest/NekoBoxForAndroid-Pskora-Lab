package io.nekohasekai.sagernet.bg

import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.ServiceConnection
import android.os.IBinder
import android.widget.Toast
import io.nekohasekai.sagernet.Action
import io.nekohasekai.sagernet.SagerNet
import io.nekohasekai.sagernet.aidl.ISagerNetService
import io.nekohasekai.sagernet.database.DataStore
import kotlinx.coroutines.*
import libcore.Libcore

/** Application-owned, not tied to a settings Fragment or Activity lifetime. */
object LogLevelReloader {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private var generation = 0L
    private var desiredLevel = 0
    private var worker: Job? = null

    fun request(context: Context, level: Int) {
        val app = context.applicationContext
        desiredLevel = level
        DataStore.logLevel = level // persist before any IPC/config rebuild
        generation++
        io.nekohasekai.sagernet.bg.proto.TemporaryLogProducers.pause()
        // Gate the UI process immediately; final clear belongs to the bg owner.
        try {
            Libcore.nekoLogReconfigure(false, true)
        } catch (e: Exception) {
            Toast.makeText(app, "Log clear failed: ${e.message}", Toast.LENGTH_LONG).show()
        }
        if (worker?.isActive == true) return
        worker = scope.launch {
            var bound = false
            val ready = CompletableDeferred<ISagerNetService>()
            val connection = object : ServiceConnection {
                override fun onServiceConnected(name: ComponentName?, binder: IBinder) {
                    ready.complete(ISagerNetService.Stub.asInterface(binder))
                }
                override fun onServiceDisconnected(name: ComponentName?) {
                    ready.completeExceptionally(IllegalStateException("Service disconnected"))
                }
                override fun onNullBinding(name: ComponentName?) {
                    ready.completeExceptionally(IllegalStateException("Service binding unavailable"))
                }
            }
            try {
                io.nekohasekai.sagernet.bg.proto.TemporaryLogProducers.quiesce()
                // Binding creates a service object if necessary, never calls startRunner.
                bound = app.bindService(Intent(app, SagerConnection.serviceClass).setAction(Action.SERVICE),
                    connection, Context.BIND_AUTO_CREATE)
                check(bound) { "Cannot bind logging service" }
                val service = withTimeout(10000) { ready.await() }
                do {
                    var target: Long
                    do {
                        target = generation
                        delay(120) // latest-value coalescing, no cancelling an in-flight teardown
                    } while (target != generation)
                    val levelToApply = desiredLevel
                    val error = withContext(Dispatchers.IO) { service.reconfigureLog(levelToApply) }
                    check(error.isEmpty()) { error }
                } while (target != generation)
            } catch (e: Exception) {
                Toast.makeText(app, "Log reload failed: ${e.message}", Toast.LENGTH_LONG).show()
            } finally {
                runCatching { Libcore.nekoLogReconfigure(DataStore.logLevel != 0, false) }
                    .onFailure { Toast.makeText(app, "Log writer failed: ${it.message}", Toast.LENGTH_LONG).show() }
                if (bound) app.unbindService(connection)
                io.nekohasekai.sagernet.bg.proto.TemporaryLogProducers.resume()
                worker = null
            }
        }
    }
}
