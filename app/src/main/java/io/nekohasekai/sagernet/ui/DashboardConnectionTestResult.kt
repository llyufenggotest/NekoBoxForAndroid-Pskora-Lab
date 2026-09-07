package io.nekohasekai.sagernet.ui

import java.net.SocketTimeoutException
import java.util.concurrent.TimeoutException

sealed class DashboardConnectionTestResult {
    data object Testing : DashboardConnectionTestResult()
    data class Success(val elapsedMs: Int) : DashboardConnectionTestResult()
    data object Timeout : DashboardConnectionTestResult()
    data class Failure(val reason: String) : DashboardConnectionTestResult()
}

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
