package io.nekohasekai.sagernet.fmt

import io.nekohasekai.sagernet.GroupType
import io.nekohasekai.sagernet.database.ProxyGroup
import io.nekohasekai.sagernet.fmt.snell.SnellBean
import java.net.URI
import java.util.Locale

/** Identity guard for signed names during subscription forceResolve, not a DNS override. */
internal fun usesOixManagedDns(bean: AbstractBean): Boolean {
    if (bean !is SnellBean || bean.oixEchTls != true || bean.version == 6) return false
    val host = bean.serverAddress?.removeSuffix(".") ?: return false
    return host.equals("cloud-nodes.com", ignoreCase = true) ||
        host.endsWith(".cloud-nodes.com", ignoreCase = true)
}

private fun cleanGroupDns(value: String?): String? = value
    ?.filterNot { it.isISOControl() }
    ?.trim()
    ?.takeIf { it.isNotEmpty() }

internal fun parseGroupDnsResolvers(raw: String): List<String>? {
    if (raw.isBlank()) return emptyList()
    val seen = LinkedHashSet<String>()
    for (part in raw.split(',')) {
        val value = part.trim()
        if (value.isEmpty() || value.any { it.isISOControl() || it.isWhitespace() }) return null
        val hasScheme = value.contains("://")
        val uri = runCatching {
            URI(if (hasScheme) value else "udp://$value")
        }.getOrNull() ?: return null
        val scheme = uri.scheme?.lowercase(Locale.ROOT) ?: return null
        if (scheme !in setOf("udp", "tcp", "tls", "https", "quic")) return null
        if (uri.rawUserInfo != null || uri.rawFragment != null || uri.host.isNullOrBlank()) return null
        if (uri.port < -1 || uri.port == 0) return null
        if (scheme != "https" && (!uri.rawPath.isNullOrEmpty() || uri.rawQuery != null)) return null
        seen += if (hasScheme) value else {
            val host = if (uri.host.contains(':')) "[${uri.host}]" else uri.host
            if (uri.port > 0) "$host:${uri.port}" else host
        }
    }
    return seen.toList()
}

internal fun normalizeGroupDnsInput(raw: String): String? =
    parseGroupDnsResolvers(raw)?.joinToString(",")

internal fun isValidGroupDnsInput(raw: String): Boolean = normalizeGroupDnsInput(raw) != null

internal fun ProxyGroup.groupDnsInput(): String =
    (if (type == GroupType.SUBSCRIPTION) cleanGroupDns(subscription?.serverDnsResolver) else null)
        ?: cleanGroupDns(customDirectDns)
        ?: ""

internal fun ProxyGroup.setGroupDnsInput(raw: String) {
    val value = normalizeGroupDnsInput(raw) ?: throw IllegalArgumentException("Invalid group DNS")
    customDirectDns = value
    (subscription ?: if (type == GroupType.SUBSCRIPTION) {
        io.nekohasekai.sagernet.database.SubscriptionBean().apply { initializeDefaultValues() }.also { subscription = it }
    } else null)?.serverDnsResolver = value
}

/** Empty new subscription field falls back to the stored legacy value, not on query failure. */
internal fun groupServerDnsResolvers(group: ProxyGroup?): List<String> =
    group?.groupDnsInput()?.let(::parseGroupDnsResolvers).orEmpty()

internal fun groupServerDnsResolver(group: ProxyGroup?): String? =
    groupServerDnsResolvers(group).firstOrNull()

/** Explicit leaf-group configuration only; blank leaves normal core/system DNS in control. */
@Suppress("UNUSED_PARAMETER")
internal fun serverDnsResolverFor(bean: AbstractBean, group: ProxyGroup?): String? =
    groupServerDnsResolver(group)

internal fun serverDnsResolversFor(bean: AbstractBean, group: ProxyGroup?): List<String> =
    groupServerDnsResolvers(group)
