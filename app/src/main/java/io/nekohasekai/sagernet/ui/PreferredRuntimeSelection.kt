package io.nekohasekai.sagernet.ui

import io.nekohasekai.sagernet.aidl.ISagerNetService
import io.nekohasekai.sagernet.database.ProfileManager
import org.json.JSONObject

/** Call from IO. IDs come only from the background config's exact tag mapping. */
internal fun preferredRuntimeLabel(service: ISagerNetService?, profileId: Long,
    onSnapshot: ((JSONObject) -> Unit)? = null): String {
    if (service == null) return "待选择"
    return try {
        val value = JSONObject(service.getPreferredSelection(profileId))
        if (value.optLong("profileId") != profileId || value.optString("session").isEmpty()) return "待选择"
        onSnapshot?.invoke(value)
        val tcp = value.optLong("tcpId")
        val name = if (tcp > 0) ProfileManager.getProfile(tcp)?.displayName() else null
        val label = if (value.optString("semantics") == "recent_tcp_success") "最近 TCP 成功" else "当前 TCP 选择"
        val udp = value.optLong("udpId")
        val udpName = if (udp > 0) ProfileManager.getProfile(udp)?.displayName() else null
        "${label}：${name ?: "待选择"}" + if (udpName != null) " · UDP：$udpName" else " · UDP 独立选择"
    } catch (_: Exception) { "待选择" }
}
