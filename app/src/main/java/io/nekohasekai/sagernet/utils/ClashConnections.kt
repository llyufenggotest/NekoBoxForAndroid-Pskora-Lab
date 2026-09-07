package io.nekohasekai.sagernet.utils

import com.google.gson.JsonObject
import com.google.gson.JsonParser
import java.net.InetAddress
import java.net.URI
import java.util.Locale

/** Parser isolated from Android so real core wire shapes can be regression tested. */
object ClashConnections {
    data class Endpoint(val url: String, val authorization: String?)
    fun endpoint(options: String): Endpoint {
        require(options.isNotBlank()) { "当前核心未发布 Clash API 配置；开启 API 后重新连接 VPN" }
        val json = JsonParser.parseString(options).asJsonObject
        val controller = json.string("external_controller")
        require(controller.isNotBlank()) { "当前核心未启用 Clash API" }
        val uri = URI("http://$controller")
        val host = uri.host?.removeSurrounding("[", "]") ?: error("无效的 Clash API 地址")
        require(uri.port in 1..65535 && uri.userInfo == null) { "无效的 Clash API 端口" }
        // Never send a controller secret to a remote host, even for custom configs.
        require(host in setOf("0.0.0.0", "::", "127.0.0.1", "::1", "localhost")) { "仅监测本机 Clash API" }
        val loopback = if (host.contains(':')) "[::1]" else "127.0.0.1"
        val secret = json.string("secret")
        return Endpoint("http://$loopback:${uri.port}/connections", secret.takeIf { it.isNotEmpty() }?.let { "Bearer $it" })
    }

    data class Snapshot(val clients: Map<String, Int>, val total: Int, val local: Int, val nonProxy: Int, val unclassified: Int, val types: Set<String>) {
        fun describe(): String {
            val rows = clients.entries.joinToString("\n") { "${it.key} · ${it.value} 条活跃连接" }
            val status = if (rows.isNotEmpty()) rows else if (unclassified > 0) "有连接但无法确认共享来源" else "当前无远端 HTTP / SOCKS 活跃连接（不代表设备已离线）"
            return "$status\n核心 $total · 本机 $local · 非代理 $nonProxy · 未分类 $unclassified" +
                if (unclassified > 0) "\n入站类型：${types.joinToString().take(160)}" else ""
        }
    }

    private fun JsonObject.string(key: String): String = get(key)?.takeIf { it.isJsonPrimitive }?.asString.orEmpty()
    private fun address(raw: String): String? {
        val s = raw.trim().removeSurrounding("[", "]").substringBefore('%')
        if (s.isEmpty() || !s.matches(Regex("[0-9a-fA-F:.]+"))) return null
        return runCatching { InetAddress.getByName(s).hostAddress?.substringBefore('%') }.getOrNull()
    }

    fun parse(body: String, localAddresses: Set<String>): Snapshot {
        val root = JsonParser.parseString(body)
        require(root.isJsonObject && root.asJsonObject.has("connections")) { "响应缺少 connections" }
        val raw = root.asJsonObject.get("connections")
        require(raw.isJsonArray || raw.isJsonNull) { "connections 不是数组" }
        val entries = if (raw.isJsonNull) emptyList() else raw.asJsonArray.toList()
        val own = localAddresses.mapNotNull(::address).toSet()
        val clients = linkedMapOf<String, Int>()
        val types = linkedSetOf<String>()
        var local = 0; var nonProxy = 0; var unknown = 0
        for (entry in entries) {
            val metadata = if (entry.isJsonObject) entry.asJsonObject.get("metadata") else null
            if (metadata == null || !metadata.isJsonObject) { unknown++; continue }
            val m = metadata.asJsonObject
            // sing-box tracker.go serializes type as InboundType + "/" + InboundTag.
            val wireType = m.string("inboundType").ifBlank { m.string("type") }
            types += wireType.ifBlank { "<missing>" }
            val type = wireType.substringBefore('/').lowercase(Locale.ROOT)
            val source = address(m.string("sourceIP"))
            if (source == null) { unknown++; continue }
            val ip = InetAddress.getByName(source)
            if (source in own || ip.isLoopbackAddress || ip.isAnyLocalAddress) { local++; continue }
            when (type) {
                "http", "https", "socks", "socks4", "socks5", "mixed" -> clients[source] = (clients[source] ?: 0) + 1
                "tun", "direct", "redirect", "tproxy" -> nonProxy++
                else -> unknown++
            }
        }
        return Snapshot(clients, entries.size, local, nonProxy, unknown, types)
    }
}
