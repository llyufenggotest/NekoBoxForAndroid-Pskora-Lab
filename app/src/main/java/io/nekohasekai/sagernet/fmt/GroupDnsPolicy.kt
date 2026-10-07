package io.nekohasekai.sagernet.fmt

import io.nekohasekai.sagernet.GroupType
import io.nekohasekai.sagernet.database.ProxyGroup
import io.nekohasekai.sagernet.fmt.snell.SnellBean

/** Identity guard for signed names during subscription forceResolve, not a DNS override. */
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

/** Explicit leaf-group configuration only; blank leaves normal core/system DNS in control. */
@Suppress("UNUSED_PARAMETER")
internal fun serverDnsResolverFor(bean: AbstractBean, group: ProxyGroup?): String? =
    groupServerDnsResolver(group)
