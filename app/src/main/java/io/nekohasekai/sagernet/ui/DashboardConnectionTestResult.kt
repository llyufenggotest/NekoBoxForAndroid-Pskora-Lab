package io.nekohasekai.sagernet.ui

import io.nekohasekai.sagernet.R
import java.net.SocketTimeoutException
import java.util.concurrent.TimeoutException

sealed class DashboardConnectionTestResult {
    data object Testing : DashboardConnectionTestResult()
    data class Success(val elapsedMs: Int) : DashboardConnectionTestResult()
    data object Timeout : DashboardConnectionTestResult()
    data class Failure(val reason: String) : DashboardConnectionTestResult()
}

internal class DashboardHealth {
    enum class Tone(@androidx.annotation.ColorRes val background: Int, @androidx.annotation.ColorRes val foreground: Int) {
        GRAY(R.color.lab_control_idle_background, R.color.lab_control_idle_foreground),
        RED(R.color.lab_control_error_background, R.color.lab_control_error_foreground),
        GREEN(R.color.lab_control_success_background, R.color.lab_control_success_foreground)
    }
    data class Ticket(val profile: Long, val generation: Long)
    private var profile = 0L
    private var generation = 0L
    private var connected = false
    var result: DashboardConnectionTestResult? = null
        private set
    val tone: Tone get() = when {
        result is DashboardConnectionTestResult.Failure || result == DashboardConnectionTestResult.Timeout -> Tone.RED
        connected && result is DashboardConnectionTestResult.Success -> Tone.GREEN
        else -> Tone.GRAY
    }

    private var networkSignature: String? = null
    fun networkChanged(signature: String): Boolean {
        if (networkSignature == signature) return false
        networkSignature = signature
        if (!connected) return false
        reset()
        return true
    }

    fun reset() { generation++; result = null }

    fun serviceChanged(target: Long, connected: Boolean, failure: DashboardConnectionTestResult? = null) {
        if (profile != target || this.connected != connected) {
            generation++
            // A service stopping after an error must not erase its diagnostic.
            if (profile != target || connected || tone != Tone.RED) result = null
        }
        profile = target
        this.connected = connected
        if (failure != null) { generation++; result = failure }
    }

    fun beginTest(target: Long): Ticket? {
        if (!connected || target != profile || result == DashboardConnectionTestResult.Testing) return null
        result = DashboardConnectionTestResult.Testing
        return Ticket(profile, ++generation)
    }

    fun complete(ticket: Ticket, value: DashboardConnectionTestResult): Boolean {
        if (!connected || ticket != Ticket(profile, generation)) return false
        result = value
        generation++
        return true
    }
}

internal fun canUpdateDashboardSubscription(isSubscription: Boolean, hasSubscription: Boolean, updating: Boolean) =
    isSubscription && hasSubscription && !updating

internal fun dashboardConnectionTestFailure(error: Throwable): DashboardConnectionTestResult {
    val raw = generateSequence(error) { it.cause }
        .mapNotNull { it.message?.trim()?.takeIf(String::isNotEmpty) }
        .firstOrNull()
        .orEmpty()
    if (error is SocketTimeoutException || error is TimeoutException ||
        raw.contains("timeout", ignoreCase = true) || raw.contains("deadline", ignoreCase = true)
    ) return DashboardConnectionTestResult.Timeout

    val sanitized = raw
        .replace(Regex("(?i)\\b(?:https?|socks5?|vless|vmess|trojan)://\\S+"), "[地址已隐藏]")
        .replace(Regex("(?i)\\b(?:password|passwd|token|secret|authorization)\\s*[:=]\\s*\\S+"), "[敏感字段已隐藏]")
        .replace(Regex("(?i)\\b[\\w.%+-]+:[^@\\s]+@"), "[凭据已隐藏]@")
        .replace(Regex("(?<![\\w.])(?:\\d{1,3}\\.){3}\\d{1,3}(?::\\d+)?"), "[地址已隐藏]")
        .replace(Regex("\\s+"), " ")
        .trim()
        .take(96)
    return DashboardConnectionTestResult.Failure(sanitized.ifBlank { "未知错误" })
}
