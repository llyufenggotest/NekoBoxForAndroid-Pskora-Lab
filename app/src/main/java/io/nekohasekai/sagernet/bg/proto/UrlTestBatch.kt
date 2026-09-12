package io.nekohasekai.sagernet.bg.proto

import io.nekohasekai.sagernet.database.ProxyEntity
import io.nekohasekai.sagernet.database.ProxyEntity.Companion.TYPE_VMESS
import io.nekohasekai.sagernet.database.ProxyEntity.Companion.TYPE_SOCKS
import io.nekohasekai.sagernet.database.ProxyEntity.Companion.TYPE_HTTP
import io.nekohasekai.sagernet.database.ProxyEntity.Companion.TYPE_SS
import io.nekohasekai.sagernet.database.ProxyEntity.Companion.TYPE_SSR
import io.nekohasekai.sagernet.database.ProxyEntity.Companion.TYPE_TROJAN
import io.nekohasekai.sagernet.database.ProxyEntity.Companion.TYPE_SSH
import io.nekohasekai.sagernet.database.ProxyEntity.Companion.TYPE_WG
import io.nekohasekai.sagernet.database.ProxyEntity.Companion.TYPE_TROJAN_GO
import io.nekohasekai.sagernet.database.ProxyEntity.Companion.TYPE_NAIVE
import io.nekohasekai.sagernet.database.ProxyEntity.Companion.TYPE_HYSTERIA
import io.nekohasekai.sagernet.database.ProxyEntity.Companion.TYPE_SHADOWTLS
import io.nekohasekai.sagernet.database.ProxyEntity.Companion.TYPE_TUIC
import io.nekohasekai.sagernet.database.ProxyEntity.Companion.TYPE_MIERU
import io.nekohasekai.sagernet.database.ProxyEntity.Companion.TYPE_ANYTLS
import io.nekohasekai.sagernet.database.ProxyEntity.Companion.TYPE_JUICITY
import io.nekohasekai.sagernet.database.ProxyEntity.Companion.TYPE_SNELL
import io.nekohasekai.sagernet.database.ProxyEntity.Companion.TYPE_XHTTP
import io.nekohasekai.sagernet.database.ProxyGroup
import io.nekohasekai.sagernet.fmt.v2ray.TunNetSelection
import io.nekohasekai.sagernet.fmt.v2ray.VMessBean
import io.nekohasekai.sagernet.fmt.v2ray.tunNetSelection
import kotlinx.coroutines.*
import java.util.concurrent.ConcurrentLinkedQueue

enum class UrlTestScope {
    ALL, UNTESTED, FAILED;
    fun includes(status: Int): Boolean = when (this) {
        ALL -> true
        UNTESTED -> status == 0
        FAILED -> status == 2 || status == 3
    }
}

// Deliberately an allow-list. New/custom types and group detours retain full parsing.
fun discoverUrlTestTunNet(
    profile: ProxyEntity,
    group: ProxyGroup?,
    fullParse: () -> TunNetSelection?,
): TunNetSelection? {
    if (group == null || group.frontProxy > 0 || group.landingProxy > 0) return fullParse()
    return when (profile.type) {
        TYPE_VMESS -> (profile.requireBean() as VMessBean).tunNetSelection()
        TYPE_SOCKS, TYPE_HTTP, TYPE_SS, TYPE_SSR, TYPE_TROJAN,
        TYPE_SSH, TYPE_WG, TYPE_TROJAN_GO, TYPE_NAIVE, TYPE_HYSTERIA,
        TYPE_SHADOWTLS, TYPE_TUIC, TYPE_MIERU, TYPE_ANYTLS, TYPE_JUICITY,
        TYPE_SNELL, TYPE_XHTTP -> null
        else -> fullParse()
    }
}

suspend fun <T> runOwnedUrlTest(
    prepare: suspend () -> Unit,
    start: suspend () -> Unit,
    request: () -> T,
    close: suspend () -> Unit,
): T = withContext(Dispatchers.IO) {
    try {
        ensureActive()
        prepare()
        ensureActive()
        start()
        ensureActive()
        val result = request()
        ensureActive()
        result
    } finally {
        withContext(NonCancellable) { close() }
    }
}

fun urlTestWorkerCount(setting: Int, size: Int): Int = setting.coerceIn(1, 16).coerceAtMost(size.coerceAtLeast(0))

suspend fun <T> runUrlTestBatch(items: List<T>, concurrency: Int, test: suspend (T) -> Unit) = coroutineScope {
    val queue = ConcurrentLinkedQueue(items)
    repeat(urlTestWorkerCount(concurrency, items.size)) {
        launch(Dispatchers.IO) {
            while (isActive) {
                val item = queue.poll() ?: break
                ensureActive()
                test(item)
            }
        }
    }
}
