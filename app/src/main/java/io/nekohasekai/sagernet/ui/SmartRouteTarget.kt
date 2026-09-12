package io.nekohasekai.sagernet.ui

import java.net.InetAddress

/** A beginner-facing target parser; advanced matchers stay compatible with RuleEntity. */
data class SmartRouteTargets(
    val domains: List<String>,
    val ips: List<String>,
    val errors: List<String> = emptyList(),
)

/** Prevent the core's domain AND IP semantics from masquerading as OR. */
fun SmartRouteTargets.simpleModeError(): String? = when {
    errors.isNotEmpty() -> "无法识别：${errors.joinToString()}"
    domains.isEmpty() && ips.isEmpty() -> "请至少填写一个域名或 IP。"
    domains.isNotEmpty() && ips.isNotEmpty() -> "域名与 IP 请分别创建两条规则；同一条高级规则中它们是同时满足，而不是任选其一。"
    else -> null
}

data class SmartRoutePreset(
    val id: String,
    val title: String,
    val subtitle: String,
    val targets: List<String>,
    /** Reserved for fallback/preferred-group integration; simple UI intentionally does not use it yet. */
    val candidateOutbounds: List<Long> = emptyList(),
)

fun smartRoutePresets() = listOf(
    SmartRoutePreset("youtube", "YouTube", "视频服务 · Domain List Community", listOf("youtube.com", "youtu.be", "googlevideo.com", "ytimg.com")),
    SmartRoutePreset("tiktok", "TikTok", "短视频 · Domain List Community", listOf("tiktok.com", "tiktokcdn.com", "tiktokcdn-us.com", "musical.ly")),
    SmartRoutePreset("telegram", "Telegram", "通讯服务 · Domain List Community", listOf("telegram.org", "telegram.me", "t.me", "telegram-cdn.org")),
    SmartRoutePreset("netflix", "Netflix", "流媒体 · Domain List Community", listOf("netflix.com", "netflix.net", "nflxvideo.net", "nflximg.net")),
    SmartRoutePreset("disney", "Disney+", "流媒体 · Domain List Community", listOf("disneyplus.com", "disney.com", "dssott.com")),
    SmartRoutePreset("x", "X", "社交服务 · Domain List Community", listOf("x.com", "twitter.com", "t.co", "twimg.com")),
    SmartRoutePreset("meta", "Instagram / Facebook", "社交服务 · Domain List Community", listOf("instagram.com", "facebook.com", "fbcdn.net")),
    SmartRoutePreset("spotify", "Spotify", "音乐服务 · Domain List Community", listOf("spotify.com", "scdn.co")),
    SmartRoutePreset("google", "Google", "搜索与服务 · Domain List Community", listOf("google.com", "googleapis.com", "gstatic.com")),
    SmartRoutePreset("ai", "AI 服务", "OpenAI · Claude · Gemini · Grok · Perplexity", listOf(
        "openai.com", "chatgpt.com", "oaistatic.com", "oaiusercontent.com", "claude.ai", "anthropic.com",
        "gemini.google.com", "generativelanguage.googleapis.com", "aistudio.google.com", "grok.com", "x.ai", "perplexity.ai")),
)

fun parseSmartRouteTargets(raw: String): SmartRouteTargets {
    val domains = linkedSetOf<String>()
    val ips = linkedSetOf<String>()
    val errors = mutableListOf<String>()
    raw.split(',', '\n', '\r', ';').map(String::trim).filter(String::isNotEmpty).forEach { source ->
        val token = source.lowercase(java.util.Locale.ROOT)
        when {
            token.startsWith("regexp:") && source.substringAfter(':').isNotBlank() -> domains += source
            (token.startsWith("geosite:") || token.startsWith("domain:") ||
                token.startsWith("full:")) && token.substringAfter(':').isNotBlank() -> domains += token
            token.startsWith("geoip:") && token.substringAfter(':').isNotBlank() -> ips += token
            isIpOrCidr(token) -> ips += token
            isDomain(token) -> domains += "domain:${token.removePrefix("*.")}" 
            else -> errors += source
        }
    }
    return SmartRouteTargets(domains.toList(), ips.toList(), errors)
}

private fun isIpOrCidr(value: String): Boolean {
    if (value.count { it == '/' } > 1) return false
    val address = value.substringBefore('/')
    val prefixText = value.substringAfter('/', "")
    val prefix = prefixText.toIntOrNull()
    if ('/' in value && (prefix == null || !prefixText.all { it in '0'..'9' })) return false
    val bits = if (':' in address) {
        // Only numeric IPv6 literals reach InetAddress: never resolve a hostname.
        if (!address.all { it in "0123456789abcdef:." }) return false
        if ('.' in address && !isIpv4(address.substringAfterLast(':'))) return false
        if (runCatching { InetAddress.getByName(address) }.isFailure) return false
        128
    } else {
        if (!isIpv4(address)) return false
        32
    }
    return prefix == null || prefix in 0..bits
}

private fun isIpv4(address: String): Boolean = address.split('.').let { parts ->
    parts.size == 4 && parts.all {
        it.isNotEmpty() && it.length <= 3 && it.all { c -> c in '0'..'9' } &&
            (it.length == 1 || !it.startsWith('0')) && (it.toIntOrNull() ?: 256) in 0..255
    }
}

private val domainLabel = Regex("^(?!-)[a-z0-9-]{1,63}(?<!-)$")
private fun isDomain(value: String): Boolean {
    val domain = value.removePrefix("*.").removeSuffix(".")
    if (domain.length !in 1..253 || !domain.contains('.') || domain.contains('/') || domain.contains("://")) return false
    if (domain.all { it.isDigit() || it == '.' }) return false
    return domain.split('.').all { domainLabel.matches(it) }
}
