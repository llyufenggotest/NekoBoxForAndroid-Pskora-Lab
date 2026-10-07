package io.nekohasekai.sagernet.fmt

import io.nekohasekai.sagernet.GroupType
import io.nekohasekai.sagernet.database.ProxyGroup
import io.nekohasekai.sagernet.fmt.snell.SnellBean

internal const val OIX_MANAGED_DNS_TAG = "dns-oix-managed"
internal const val OIX_MANAGED_DNS_ADDRESS = "tcp://124.221.68.73:1053"

internal fun usesOixManagedDns(bean: AbstractBean): Boolean {
    if (bean !is SnellBean || bean.oixEchTls != true || bean.version == 6) return false
    val host = bean.serverAddress?.removeSuffix(".") ?: return false
    return host.equals("cloud-nodes.com", ignoreCase = true) ||
        host.endsWith(".cloud-nodes.com", ignoreCase = true)
}

/** Empty new subscription field falls back to the stored legacy value, not on query failure. */
internal fun groupServerDnsResolver(group: ProxyGroup?): String? {
    fun clean(value: String?) = value?.filterNot { it.isISOControl() }?.trim()?.takeIf { it.isNotEmpty() }
    return (if (group?.type == GroupType.SUBSCRIPTION) clean(group.subscription?.serverDnsResolver) else null)
        ?: clean(group?.customDirectDns)
}

/** Oix signed names must never go to an ordinary resolver. Other nodes use their own group only. */
internal fun serverDnsResolverFor(bean: AbstractBean, group: ProxyGroup?): String? =
    if (usesOixManagedDns(bean)) OIX_MANAGED_DNS_ADDRESS else groupServerDnsResolver(group)
