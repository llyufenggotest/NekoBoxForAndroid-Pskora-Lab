package io.nekohasekai.sagernet.fmt.oppa

import com.google.gson.JsonParser
import io.nekohasekai.sagernet.database.DataStore
import moe.matsuri.nb4a.SingBoxOptions
import java.net.URI
import java.net.URLDecoder
import java.net.URLEncoder
import java.util.Base64

data class OppaProvider(
    val name: String,
    val apiToken: String,
    val apiEndpoint: String,
    val encryptionKey: String,
)

fun parseOppaProvider(link: String): OppaProvider {
    require(link.startsWith("oppa://")) { "invalid Oppa provider link" }
    val payload = link.removePrefix("oppa://")
    require(payload.isNotBlank() && !payload.contains('@')) { "invalid Oppa provider payload" }
    val decoded = runCatching {
        Base64.getUrlDecoder().decode(payload.padEnd((payload.length + 3) / 4 * 4, '='))
    }.getOrElse { throw IllegalArgumentException("invalid Oppa provider encoding", it) }
    val json = runCatching { JsonParser.parseString(String(decoded, Charsets.UTF_8)).asJsonObject }
        .getOrElse { throw IllegalArgumentException("invalid Oppa provider JSON", it) }
    fun string(name: String) = json.get(name)?.takeUnless { it.isJsonNull }?.asString?.trim().orEmpty()
    val name = string("name")
    val apiToken = string("apiToken")
    val apiEndpoint = string("apiEndPoint").removePrefix("https://").trimEnd('/')
    val encryptionKey = string("encryptionKey")
    require(name.isNotEmpty()) { "missing Oppa provider name" }
    require(apiToken.isNotEmpty()) { "missing Oppa provider token" }
    require(apiEndpoint.isNotEmpty()) { "missing Oppa provider endpoint" }
    require(encryptionKey.isNotEmpty()) { "missing Oppa provider encryption key" }
    return OppaProvider(name, apiToken, apiEndpoint, encryptionKey)
}

fun parseOppaNode(url: String): OppaBean {
    require(url.startsWith("oppa://")) { "invalid Oppa node link" }
    val uri = runCatching { URI(url) }.getOrElse { throw IllegalArgumentException("invalid Oppa node link", it) }
    require(uri.scheme.equals("oppa", true) && !uri.host.isNullOrBlank() && uri.port in 1..65535) {
        "invalid Oppa node link"
    }
    val password = percentDecode(uri.rawUserInfo.orEmpty())
    requireValidPassword(password)
    val query = parseQuery(uri.rawQuery)
    return OppaBean().apply {
        serverAddress = uri.host.trim('[', ']')
        serverPort = uri.port
        this.password = password
        preConnect = query["preconnect"]?.toIntOrNull()?.coerceIn(0, 64) ?: 8
        sni = query["sni"].orEmpty()
        allowInsecure = query["insecure"]?.let { it == "1" || it.equals("true", true) } ?: false
        certificatePinSHA256 = query["pin_sha256"].orEmpty()
        name = percentDecode(uri.rawFragment.orEmpty())
        initializeDefaultValues()
    }
}

fun buildSingBoxOutboundOppaBean(bean: OppaBean): SingBoxOptions.Outbound_OppaOptions {
    bean.initializeDefaultValues()
    return SingBoxOptions.Outbound_OppaOptions().apply {
        type = "oppa"
        server = bean.serverAddress
        server_port = bean.serverPort
        password = bean.password
        pre_connect = bean.preConnect
        pin_cert_sha256 = bean.certificatePinSHA256.ifBlank { null }
        tls = SingBoxOptions.OutboundTLSOptions().apply {
            enabled = true
            server_name = bean.sni.ifBlank { null }
            insecure = bean.allowInsecure || DataStore.globalAllowInsecure
            alpn = null
        }
    }
}

fun OppaBean.toUri(): String {
    initializeDefaultValues()
    requireValidPassword(password)
    val host = if (serverAddress.contains(':')) "[$serverAddress]" else serverAddress
    val query = buildList {
        if (preConnect != 8) add("preconnect=${percentEncode(preConnect.toString())}")
        if (sni.isNotBlank()) add("sni=${percentEncode(sni)}")
        if (allowInsecure) add("insecure=1")
        if (certificatePinSHA256.isNotBlank()) add("pin_sha256=${percentEncode(certificatePinSHA256)}")
    }.joinToString("&")
    return buildString {
        append("oppa://")
        append(percentEncode(password))
        append('@')
        append(host)
        append(':')
        append(serverPort)
        if (query.isNotEmpty()) append('?').append(query)
        if (name.isNotBlank()) append('#').append(percentEncode(name))
    }
}

private fun requireValidPassword(password: String) {
    require(password.isNotEmpty() && password.toByteArray(Charsets.UTF_8).size <= 4096) {
        "Oppa password must contain 1–4096 UTF-8 bytes"
    }
}

private fun parseQuery(rawQuery: String?): Map<String, String> {
    if (rawQuery.isNullOrBlank()) return emptyMap()
    return rawQuery.split('&').mapNotNull { part ->
        val pieces = part.split('=', limit = 2)
        if (pieces.isEmpty() || pieces[0].isEmpty()) null
        else percentDecode(pieces[0]) to percentDecode(pieces.getOrElse(1) { "" })
    }.toMap()
}

private fun percentEncode(value: String): String =
    URLEncoder.encode(value, Charsets.UTF_8.name()).replace("+", "%20")

private fun percentDecode(value: String): String =
    URLDecoder.decode(value.replace("+", "%2B"), Charsets.UTF_8.name())
