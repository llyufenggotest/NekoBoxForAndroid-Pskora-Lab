package moe.matsuri.nb4a.proxy.config

/** Pure validation/resolution model shared by the editor and config builder. */
data class PreferredGroupSpec(
    val memberIds: List<Long> = emptyList(),
    val sourceGroupIds: List<Long> = emptyList(),
    val intervalSeconds: Int = 300,
    val minDelayMilliseconds: Int = 0,
    val mode: String = PREFERRED_MODE_LATENCY,
    val excludedMemberIds: List<Long> = emptyList(),
) {
    /** Tombstones apply to explicit and dynamic references; never mutate source membership. */
    fun resolvedIds(groups: Map<Long, List<Long>>): List<Long> =
        (memberIds + sourceGroupIds.flatMap { groups[it].orEmpty() }).distinct()
            .filterNot { it in excludedMemberIds }

    fun withoutMember(id: Long): PreferredGroupSpec {
        require(id > 0)
        return copy(memberIds = memberIds.filterNot { it == id },
            excludedMemberIds = (excludedMemberIds + id).distinct())
    }
    fun validate(existingIds: Set<Long>, existingGroupIds: Set<Long>, ownerGroupId: Long? = null): List<Long> {
        require(memberIds.isNotEmpty() || sourceGroupIds.isNotEmpty()) { "优选分组至少需要一个节点或来源分组" }
        require(memberIds.all { it > 0 && it in existingIds }) { "优选分组引用了已删除或无效节点" }
        require(sourceGroupIds.all { it > 0 && it in existingGroupIds }) { "优选分组引用了已删除或无效来源分组" }
        require(memberIds.distinct().size == memberIds.size) { "优选分组包含重复节点" }
        require(sourceGroupIds.distinct().size == sourceGroupIds.size) { "优选分组包含重复来源分组" }
        require(ownerGroupId == null || ownerGroupId !in sourceGroupIds) { "来源分组不能包含优选分组自身" }
        require(intervalSeconds in 1..86400) { "测速周期必须为 1–86400 秒" }
        require(minDelayMilliseconds in 0..65535) { "切换容差必须为 0–65535 毫秒" }
        require(mode == PREFERRED_MODE_LATENCY || mode == PREFERRED_MODE_STABLE) { "未知优选模式：$mode" }
        return memberIds
    }
}

const val PREFERRED_MODE_LATENCY = "latency"
const val PREFERRED_MODE_STABLE = "stable"
const val EXTRA_PREFERRED_MODE = "preferredMode"

/** Release gate: enable only after the packaged AAR passes native fallback behavior
 * and binary-identity verification. Source presence/version strings are insufficient. */
const val VERIFIED_NATIVE_DIAL_FALLBACK = true

const val EXTRA_PREFERRED_NAME = "preferredGroupName"
const val EXTRA_PREFERRED_MEMBER_IDS = "preferredMemberIds"
const val EXTRA_PREFERRED_SOURCE_GROUP_IDS = "preferredSourceGroupIds"
const val EXTRA_PREFERRED_INTERVAL_SECONDS = "preferredIntervalSeconds"
const val EXTRA_PREFERRED_MIN_DELAY_MILLISECONDS = "preferredMinDelayMilliseconds"

fun nativeUrlTest(tag: String, members: List<String>, intervalSeconds: Int, minDelayMilliseconds: Int,
    mode: String = PREFERRED_MODE_LATENCY,
    verifiedDialFallback: Boolean = VERIFIED_NATIVE_DIAL_FALLBACK,
): Map<String, Any> {
    require(tag.isNotBlank() && members.isNotEmpty() && members.none { it.isBlank() || it == tag })
    require(intervalSeconds in 1..86400 && minDelayMilliseconds in 0..65535)
    require(mode == PREFERRED_MODE_LATENCY || mode == PREFERRED_MODE_STABLE) { "未知优选模式：$mode" }
    require(mode != PREFERRED_MODE_STABLE || verifiedDialFallback) { "稳定备用需要经验证支持 fallback 的新核心" }
    return linkedMapOf<String, Any>("type" to "urltest", "tag" to tag, "outbounds" to members.distinct(),
        "interval" to "${intervalSeconds}s", "tolerance" to minDelayMilliseconds).apply {
        if (verifiedDialFallback) {
            put("dial_fallback", true)
            put("fallback_mode", mode)
        }
    }
}

/** A path-local guard permits shared descendants but rejects cycles with a useful path. */
class PreferredReferenceGuard {
    private val path = linkedSetOf<Long>()
    fun <T> visit(id: Long, block: () -> T): T {
        require(path.size < 64) { "优选引用层级过深" }
        require(path.add(id)) { "优选/链式引用循环: ${path.joinToString(" → ")} → $id" }
        return try { block() } finally { path.remove(id) }
    }
}
