package io.nekohasekai.sagernet.fmt

/** Resolve a shared hop before emitting any predecessor detour or route reference. */
internal fun resolveSharedOutboundTag(
    proposedTag: String,
    needsGlobal: Boolean,
    profileId: Long,
    globalOutbounds: Map<Long, String>,
): String = if (needsGlobal) globalOutbounds[profileId] ?: proposedTag else proposedTag
