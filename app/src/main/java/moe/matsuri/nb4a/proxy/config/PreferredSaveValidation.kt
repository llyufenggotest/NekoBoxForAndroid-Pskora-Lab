package moe.matsuri.nb4a.proxy.config

import io.nekohasekai.sagernet.database.ProxyEntity
import io.nekohasekai.sagernet.database.ProxyGroup

/** Validate the resulting reference graph before writing a new or edited preferred group. */
fun validatePreferredSave(candidate: ProxyEntity, profiles: List<ProxyEntity>, groups: List<ProxyGroup>) {
    val id = if (candidate.id > 0) candidate.id else
        generateSequence(1L) { it + 1 }.first { next -> profiles.none { it.id == next } }
    val proposed = candidate.copy(id = id)
    val snapshot = profiles.filter { it.id != id } + proposed
    val resolver = PreferredGroupResolver(snapshot, groups)
    snapshot.filter { it.configBean?.type == 2 }.forEach { profile ->
        try { resolver.validate(profile) }
        catch (e: IllegalArgumentException) {
            throw IllegalArgumentException("${profile.displayName()}: ${e.message}", e)
        }
    }
}
