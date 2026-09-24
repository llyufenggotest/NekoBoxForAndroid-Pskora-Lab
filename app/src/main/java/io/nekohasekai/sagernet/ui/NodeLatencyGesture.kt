package io.nekohasekai.sagernet.ui

import android.os.SystemClock
import io.nekohasekai.sagernet.SagerNet
import io.nekohasekai.sagernet.bg.proto.UrlTest
import io.nekohasekai.sagernet.database.ProxyEntity
import okhttp3.internal.closeQuietly
import java.net.InetSocketAddress
import java.net.Socket

internal enum class NodeLatencyMode { URL_TEST, TCP }

internal fun nodeLatencyMode(longPress: Boolean): NodeLatencyMode =
    if (longPress) NodeLatencyMode.TCP else NodeLatencyMode.URL_TEST

internal suspend fun runNodeLatency(profile: ProxyEntity, mode: NodeLatencyMode): Int = when (mode) {
    NodeLatencyMode.URL_TEST -> UrlTest().doTest(profile)
    NodeLatencyMode.TCP -> {
        val bean = profile.requireBean()
        require(bean.canTCPing()) { "TCP latency test is not supported" }
        val socket = SagerNet.underlyingNetwork?.socketFactory?.createSocket() ?: Socket()
        try {
            socket.soTimeout = 3000
            socket.bind(InetSocketAddress(0))
            val startedAt = SystemClock.elapsedRealtime()
            socket.connect(InetSocketAddress(bean.serverAddress, bean.serverPort), 3000)
            (SystemClock.elapsedRealtime() - startedAt).toInt()
        } finally {
            socket.closeQuietly()
        }
    }
}
