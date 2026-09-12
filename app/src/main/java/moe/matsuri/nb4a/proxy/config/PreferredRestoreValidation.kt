package moe.matsuri.nb4a.proxy.config

import io.nekohasekai.sagernet.database.ProxyEntity
import io.nekohasekai.sagernet.database.ProxyGroup

/** Validate the incoming full replacement before the caller deletes existing rows. */
fun validatePreferredRestoreSnapshot(profiles: List<ProxyEntity>, groups: List<ProxyGroup>) {
    require(profiles.all { it.id > 0 } && profiles.map { it.id }.distinct().size == profiles.size) {
        "备份节点 ID 无效或重复"
    }
    require(groups.all { it.id > 0 } && groups.map { it.id }.distinct().size == groups.size) {
        "备份分组 ID 无效或重复"
    }
    val resolver = PreferredGroupResolver(profiles, groups)
    profiles.filter { it.configBean?.type == 2 }.forEach { resolver.validate(it) }
}
