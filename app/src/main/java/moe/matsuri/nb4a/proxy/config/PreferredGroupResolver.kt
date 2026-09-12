package moe.matsuri.nb4a.proxy.config

import io.nekohasekai.sagernet.database.ProxyEntity
import io.nekohasekai.sagernet.database.ProxyGroup
import io.nekohasekai.sagernet.fmt.internal.ChainBean
import io.nekohasekai.sagernet.fmt.v2ray.VMessBean
import io.nekohasekai.sagernet.fmt.v2ray.isTunNet

fun ConfigBean.preferredSpec() = PreferredGroupSpec(preferredMemberIds.orEmpty(),
    preferredSourceGroupIds.orEmpty(), preferredIntervalSeconds ?: 300, preferredMinDelayMilliseconds ?: 0,
    preferredMode ?: PREFERRED_MODE_LATENCY, preferredExcludedMemberIds.orEmpty())

/** Immutable DB membership snapshot per validation/build; never silently drop a candidate. */
class PreferredGroupResolver(profiles: List<ProxyEntity>, groups: List<ProxyGroup>) {
    private val profiles = profiles.associateBy { it.id }
    private val groups = groups.associateBy { it.id }
    private val guard = PreferredReferenceGuard()

    fun members(entity: ProxyEntity): List<ProxyEntity> {
        val spec = (entity.requireBean() as ConfigBean).preferredSpec()
        spec.validate(profiles.keys, groups.keys, entity.groupId)
        val sourceIds = linkedMapOf<Long, List<Long>>()
        spec.sourceGroupIds.forEach { gid ->
            val found = profiles.values.filter { it.groupId == gid }.sortedBy { it.userOrder }
            require(found.isNotEmpty()) { "来源分组 ${groups[gid]?.displayName()} 没有节点" }
            sourceIds[gid] = found.map { it.id }
        }
        return spec.resolvedIds(sourceIds).map { requireNotNull(profiles[it]) { "优选节点 $it 已删除" } }.also {
            require(it.isNotEmpty()) { "优选分组没有可用引用，请重新选择成员" }
        }
    }

    fun candidateUnsupportedReason(entity: ProxyEntity): String? =
        if (entity.configBean?.type == 2) "不支持嵌套优选分组，请直接选择其中的节点"
        else unsupportedReason(entity)

    fun validate(entity: ProxyEntity) { visit(entity, false) }
    fun unsupportedReason(entity: ProxyEntity): String? = try { validate(entity); null }
        catch (e: IllegalArgumentException) { e.message ?: "不支持此节点" }

    private fun visit(entity: ProxyEntity, inChain: Boolean) {
        guard.visit(entity.id) {
            val bean = entity.requireBean()
            require(!entity.needExternal()) { "${entity.displayName()}: 外部插件不支持安全自动热切换" }
            require(bean !is VMessBean || !bean.isTunNet()) { "${entity.displayName()}: TunNet 依赖运行时快照，需要重启，不支持自动热切换" }
            require(bean.customOutboundJson.isNullOrBlank()) { "${entity.displayName()}: 自定义出站覆盖无法保证安全热切换" }
            if (bean is ConfigBean) {
                require(bean.type == 2) { "${entity.displayName()}: 自定义配置无法验证候选引用，不支持自动热切换" }
                require(!inChain) { "链式节点不能嵌入优选分组" }
                members(entity).forEach {
                    require(it.configBean?.type != 2) { "${it.displayName()}: 当前核心不支持嵌套优选分组，请直接选择其中的节点或普通来源分组" }
                    visit(it, false)
                }
            } else if (bean is ChainBean) {
                require(bean.proxies.isNotEmpty()) { "${entity.displayName()}: 空链式节点" }
                bean.proxies.forEach { id -> visit(requireNotNull(profiles[id]) { "链式引用节点 $id 已删除" }, true) }
            }
            if (!inChain) {
                val group = requireNotNull(groups[entity.groupId]) { "节点所属分组已删除" }
                val detours = listOf(group.frontProxy, group.landingProxy).filter { it > 0 }
                require(bean !is ConfigBean || detours.isEmpty()) { "优选分组自身不支持前置/落地代理；请在候选节点上配置链式代理" }
                detours.forEach { id ->
                    val hop = requireNotNull(profiles[id]) { "前置/落地节点 $id 已删除" }
                    require(hop.requireBean() !is ChainBean) { "前置/落地代理不能是嵌套链式节点" }
                    visit(hop, true)
                }
            }
        }
    }
}
