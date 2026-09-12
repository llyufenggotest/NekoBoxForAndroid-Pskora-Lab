package io.nekohasekai.sagernet.bg

import android.net.Network
import android.os.Build
import android.os.Process
import io.nekohasekai.sagernet.SagerNet
import io.nekohasekai.sagernet.ktx.Logs
import java.util.concurrent.atomic.AtomicInteger

/** Narrow injectable boundary for the same persistent neko.log writer used by SendLog. */
internal fun interface VpnUnderlyingDiagnosticWriter {
    fun write(line: String)
}

internal class VpnUnderlyingDiagnosticSink(
    private val writer: VpnUnderlyingDiagnosticWriter = VpnUnderlyingDiagnosticWriter(Logs::i),
) {
    @Synchronized
    fun write(line: String) = writer.write(line)
}

/** Android-only sink. Never accepts or logs addresses, names, DNS, URLs, or package data. */
internal object VpnUnderlyingLiveDiagnostic {
    private const val MAX_EVENTS_PER_PROCESS = 512
    private val emitted = AtomicInteger()
    private val model = VpnUnderlyingDiagnosticModel()
    private val sink = VpnUnderlyingDiagnosticSink()

    private fun hash(value: Long): Long {
        var x = value
        x = (x xor (x ushr 33)) * -49064778989728563L
        x = (x xor (x ushr 33)) * -4265267296055464877L
        return (x xor (x ushr 33)) and Long.MAX_VALUE
    }

    private fun networkHash(network: Network?): Long = network?.let { hash(it.networkHandle) } ?: 0
    private fun threadHash(): Long = hash(Process.myTid().toLong())
    private fun emit(event: VpnUnderlyingDiagnosticModel.Event) {
        if (emitted.getAndIncrement() < MAX_EVENTS_PER_PROCESS) {
            sink.write(event.render())
        }
    }

    fun recordLiveSet(network: Network, call: (Array<Network>) -> Boolean) {
        val requested = networkHash(network)
        try {
            val result = if (call(arrayOf(network))) VpnUnderlyingDiagnosticModel.Result.TRUE
                else VpnUnderlyingDiagnosticModel.Result.FALSE
            emit(model.recordSetRequest(requested, result, threadHash()))
        } catch (error: Throwable) {
            emit(model.recordSetRequest(requested, VpnUnderlyingDiagnosticModel.Result.EXCEPTION,
                threadHash(), exceptionClass = exceptionClass(error)))
            throw error
        }
    }

    /** Documents the existing no-call behavior; it intentionally does not change it. */
    fun recordOfflineNoRequest() { emit(model.recordOfflineNoRequest(threadHash())) }

    /** Records the I variant's initial omission without invoking Builder.setUnderlyingNetworks. */
    fun recordInitialOmitted(network: Network?) {
        emit(model.recordInitialOmitted(networkHash(network), threadHash()))
    }

    /** Records a bounded numeric marker without invoking framework setUnderlyingNetworks. */
    fun recordSuppressedEstablishedUpdate(network: Network?) {
        emit(model.recordSuppressedEstablishedUpdate(networkHash(network), threadHash()))
    }

    fun recordConnectivityCallback(callbackCode: Int, selected: Network?) {
        val cm = SagerNet.connectivity
        val active = if (Build.VERSION.SDK_INT >= 23) cm.activeNetwork else null
        val activeCaps = active?.let(cm::getNetworkCapabilities)
        val selectedCaps = selected?.let(cm::getNetworkCapabilities)
        val activeIsVpn = activeCaps?.hasTransport(android.net.NetworkCapabilities.TRANSPORT_VPN) == true
        emit(model.recordCallback(
            activeHash = networkHash(active),
            selectedHash = networkHash(selected),
            activeIsVpn = activeIsVpn,
            isDefault = active == selected || activeIsVpn,
            validated = selectedCaps?.hasCapability(android.net.NetworkCapabilities.NET_CAPABILITY_VALIDATED) == true,
            callbackCode = callbackCode,
            declaredUnderlyingHash = networkHash(SagerNet.underlyingNetwork),
            threadHash = threadHash(),
        ))
    }

    private fun exceptionClass(error: Throwable): Int = when (error) {
        is SecurityException -> 1
        is IllegalStateException -> 2
        is IllegalArgumentException -> 3
        else -> 9
    }
}
