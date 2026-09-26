package io.nekohasekai.sagernet.database

import io.nekohasekai.sagernet.GroupType
import io.nekohasekai.sagernet.group.GroupUpdater
import moe.matsuri.nb4a.proxy.config.ConfigBean
import moe.matsuri.nb4a.proxy.config.PreferredGroupResolver
import moe.matsuri.nb4a.proxy.config.validatePreferredSave
import moe.matsuri.nb4a.proxy.config.preferredSpec
import java.util.concurrent.Callable
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.withTimeoutOrNull
import kotlinx.coroutines.withContext

/** Independent groups own only a managed container. Candidate IDs always belong to their sources. */
object PreferredGroupStore {
    /** Re-read dynamic source groups before a preferred container is connected. */
    suspend fun syncSources(groupId: Long): Boolean {
        val sourceIds = withContext(Dispatchers.IO) {
            val owner = container(groupId) ?: return@withContext emptyList<Long>()
            owner.configBean?.preferredSourceGroupIds.orEmpty()
        }
        if (sourceIds.isEmpty()) return true
        val groups = withContext(Dispatchers.IO) {
            sourceIds.mapNotNull { SagerDatabase.groupDao.getById(it) }
                .filter { it.type == GroupType.SUBSCRIPTION && it.subscription != null }
        }
        withContext(Dispatchers.IO) {
            groups.forEach { group ->
                if (GroupUpdater.updating.contains(group.id)) {
                    withTimeoutOrNull(15_000L) {
                        while (GroupUpdater.updating.contains(group.id)) delay(50L)
                    }
                } else {
                    GroupUpdater.executeUpdate(group, byUser = false)
                }
            }
        }
        return withContext(Dispatchers.IO) {
            SagerDatabase.instance.runInTransaction(Callable {
                val owner = container(groupId) ?: return@Callable false
                val bean = owner.configBean ?: return@Callable false
                val existingIds = SagerDatabase.proxyDao.getAll().map { it.id }.toHashSet()
                val existingGroupIds = SagerDatabase.groupDao.allGroups().map { it.id }.toHashSet()
                val spec = bean.preferredSpec()
                val pruned = spec.pruned(existingIds, existingGroupIds)
                if (pruned != spec) {
                    val cleaned = bean.clone()
                    cleaned.preferredMemberIds = pruned.memberIds
                    cleaned.preferredSourceGroupIds = pruned.sourceGroupIds
                    cleaned.preferredExcludedMemberIds = pruned.excludedMemberIds
                    owner.putBean(cleaned)
                    SagerDatabase.proxyDao.updateProxy(owner)
                }
                val healed = container(groupId) ?: return@Callable false
                runCatching {
                    PreferredGroupResolver(
                        SagerDatabase.proxyDao.getAll(), SagerDatabase.groupDao.allGroups(),
                    ).validate(healed)
                }.isSuccess
            })
        }
    }
    fun container(groupId: Long): ProxyEntity? =
        SagerDatabase.proxyDao.getByGroup(groupId).singleOrNull { it.configBean?.type == 2 }

    /** Data migration, deliberately preserving profile IDs used by routing rules. Safe to repeat. */
    fun migrateLegacy() = SagerDatabase.instance.runInTransaction {
        val groups = SagerDatabase.groupDao.allGroups().associateBy { it.id }
        SagerDatabase.proxyDao.getAll().filter { it.configBean?.type == 2 }.forEach { profile ->
            if (groups[profile.groupId]?.type != GroupType.PREFERRED) {
                val group = ProxyGroup(name = profile.displayName(), type = GroupType.PREFERRED,
                    userOrder = SagerDatabase.groupDao.nextOrder() ?: 1L)
                group.id = SagerDatabase.groupDao.createGroup(group)
                profile.groupId = group.id
                profile.userOrder = 1L
                SagerDatabase.proxyDao.updateProxy(profile)
            }
        }
    }

    fun save(bean: ConfigBean, expected: ProxyEntity?): ProxyEntity =
        SagerDatabase.instance.runInTransaction(Callable {
            val current = expected?.let { old ->
                requireNotNull(SagerDatabase.proxyDao.getById(old.id)) { "优选分组已删除" }.also {
                    require(it.groupId == old.groupId && it.configBean == old.configBean &&
                        it.configBean?.name == old.configBean?.name) {
                        "优选分组已被修改，请重新打开"
                    }
                }
            }
            val existingGroup = current?.let { SagerDatabase.groupDao.getById(it.groupId) }
            val group = existingGroup?.takeIf { it.type == GroupType.PREFERRED }?.copy(name = bean.name)
                ?: ProxyGroup(name = bean.name, type = GroupType.PREFERRED,
                    userOrder = SagerDatabase.groupDao.nextOrder() ?: 1L)
            if (group.id == 0L) group.id = SagerDatabase.groupDao.createGroup(group)
            else SagerDatabase.groupDao.updateGroup(group)
            val candidate = (current?.copy(groupId = group.id) ?: ProxyEntity(groupId = group.id, userOrder = 1L)).putBean(bean)
            validatePreferredSave(candidate, SagerDatabase.proxyDao.getAll(), SagerDatabase.groupDao.allGroups())
            if (candidate.id == 0L) candidate.id = SagerDatabase.proxyDao.addProxy(candidate)
            else SagerDatabase.proxyDao.updateProxy(candidate)
            candidate
        })

    /** Remove only container references, including persistent exclusions for dynamic sources. */
    fun removeReference(groupId: Long, memberId: Long): ProxyEntity =
        SagerDatabase.instance.runInTransaction(Callable {
            val owner = requireNotNull(container(groupId)) { "优选分组已删除" }
            val bean = owner.configBean!!.clone()
            require(memberId > 0 && rows(groupId).second.any { it.first?.id == memberId }) { "成员已变更，请刷新" }
            val removed = bean.preferredSpec().withoutMember(memberId)
            bean.preferredMemberIds = removed.memberIds
            bean.preferredExcludedMemberIds = removed.excludedMemberIds
            owner.putBean(bean)
            SagerDatabase.proxyDao.updateProxy(owner)
            require(container(groupId)?.configBean?.preferredExcludedMemberIds?.contains(memberId) == true)
            owner
        })

    /** Read current source rows, retaining their IDs, traffic and test state. Missing references remain visible. */
    fun rows(groupId: Long): Pair<ProxyEntity?, List<Pair<ProxyEntity?, String>>> {
        val owner = container(groupId) ?: return null to emptyList()
        val bean = owner.configBean!!
        val rows = linkedMapOf<Long, Pair<ProxyEntity?, String>>()
        bean.preferredMemberIds.orEmpty().forEach { id ->
            val node = SagerDatabase.proxyDao.getById(id)
            rows[id] = node to (node?.let { SagerDatabase.groupDao.getById(it.groupId)?.displayName() }
                ?: "节点 #$id 已删除，请编辑引用")
        }
        bean.preferredSourceGroupIds.orEmpty().forEach { id ->
            val source = SagerDatabase.groupDao.getById(id)
            val nodes = SagerDatabase.proxyDao.getByGroup(id)
            if (nodes.isEmpty()) rows[-id] = null to "${source?.displayName() ?: "分组 #$id 已删除"}：无可用节点"
            nodes.forEach { rows[it.id] = it to (source?.displayName() ?: "来源已删除") }
        }
        return owner to rows.filterKeys { it !in bean.preferredExcludedMemberIds.orEmpty() }.values.toList()
    }
}
