package io.nekohasekai.sagernet.fmt.v2ray

import okhttp3.HttpUrl.Companion.toHttpUrl

private val pureIdentity = Regex("^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}#pure$", RegexOption.IGNORE_CASE)

fun VMessBean.isPure(): Boolean {
 if (!isVLESS || !uuid.contains("#pure", ignoreCase = true)) return false
 require(pureIdentity.matches(uuid)) { "Pure: canonical UUID#pure required; mixed private modes unsupported" }
 return true
}

fun parsePureLink(link: String): VMessBean {
 require(link.startsWith("xless://", ignoreCase = true))
 val url = ("https://" + link.substringAfter("://")).toHttpUrl()
 require(url.password.isEmpty()) { "Pure: password field unsupported" }
 val id = if (url.username.contains('#')) url.username else url.username + "#pure"
 require(pureIdentity.matches(id)) { "Pure: canonical UUID required" }
 return VMessBean().apply {
  alterId = -1
  parseDuckSoft(url)
  uuid = id
  if (url.queryParameter("type") == null) type = "ws"
  if (url.queryParameter("security") == null) security = "tls"
  if (url.queryParameter("path") == null) path = "/websocket"
  if (url.queryParameter("packetEncoding") == null) packetEncoding = 0
  initializeDefaultValues()
  validatePureOptions()
 }
}

fun VMessBean.validatePureOptions() {
 if (!isPure()) return
 require(type == "ws" && security == "tls") { "Pure: requires TLS WebSocket" }
 require(path.isBlank() || path == "/websocket") { "Pure: WebSocket path must be /websocket" }
 require(host.isBlank() && wsMaxEarlyData == 0 && earlyDataHeaderName.isBlank()) { "Pure: custom WebSocket headers/early data unsupported" }
 require(encryption.isBlank() || encryption == "auto") { "Pure: Vision/flow unsupported" }
 require(vlessEncryption.isBlank() || vlessEncryption == "none") { "Pure: VLESS encryption unsupported" }
 require(utlsFingerprint.isBlank() && realityPubKey.isBlank() && !enableECH) { "Pure: uTLS/Reality/ECH unsupported" }
 require(alpn.isBlank() || alpn == "http/1.1") { "Pure: ALPN must be http/1.1" }
 require(packetEncoding == 0) { "Pure: UDP/XUDP unsupported; select no packet encoding" }
}
