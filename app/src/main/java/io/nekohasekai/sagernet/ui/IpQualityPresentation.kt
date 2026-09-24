package io.nekohasekai.sagernet.ui

internal fun countryFlag(code: String): String {
    val normalized = code.trim().uppercase()
    if (normalized.length != 2 || normalized.any { it !in 'A'..'Z' }) return "🌐"
    return normalized.map { char ->
        Character.toChars(0x1F1E6 + (char.code - 'A'.code)).concatToString()
    }.joinToString("")
}

internal fun ipSourceChinese(value: String): String = when (value.lowercase()) {
    "native" -> "原生 IP"
    "broadcast" -> "广播 IP"
    "anycast" -> "任播 IP"
    else -> "未知"
}

internal fun ipAttributeChinese(value: String): String = when (value.lowercase()) {
    "residential" -> "住宅 IP"
    "datacenter" -> "机房 IP"
    "proxy_vpn" -> "代理 / VPN"
    "mobile" -> "移动网络"
    else -> "未知"
}

internal fun ipQualityTierChinese(value: String): String = when (value.lowercase()) {
    "green" -> "纯净"
    "yellow" -> "中性"
    "red" -> "风险"
    else -> "未知"
}
