package moe.matsuri.nb4a.proxy.anytls

import io.nekohasekai.sagernet.ktx.blankAsNull
import io.nekohasekai.sagernet.ktx.linkBuilder
import io.nekohasekai.sagernet.ktx.toLink
import io.nekohasekai.sagernet.ktx.urlSafe
import moe.matsuri.nb4a.SingBoxOptions
import moe.matsuri.nb4a.utils.listByLineOrComma
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull

fun buildSingBoxOutboundAnyTLSBean(bean: AnyTLSBean): SingBoxOptions.Outbound_AnyTLSOptions {
    return SingBoxOptions.Outbound_AnyTLSOptions().apply {
        type = "anytls"
        server = bean.serverAddress
        server_port = bean.serverPort
        password = bean.password

        // 动态读取闲置连接配置
        min_idle_session = bean.minIdleSession ?: 0
        idle_session_check_interval = bean.idleSessionCheckInterval?.let { "${it}s" } ?: "30s"
        idle_session_timeout = bean.idleSessionTimeout?.let { "${it}s" } ?: "30s"

        tls = SingBoxOptions.OutboundTLSOptions().apply {
            enabled = true
            server_name = bean.sni.blankAsNull()
            if (bean.allowInsecure) insecure = true
            
            bean.certificates.blankAsNull()?.let {
                certificate = it
            }
            
            var fp = bean.utlsFingerprint.blankAsNull()
            if (!bean.realityPubKey.isNullOrBlank()) {
                reality = SingBoxOptions.OutboundRealityOptions().apply {
                    enabled = true
                    public_key = bean.realityPubKey
                    short_id = bean.realityShortId
                }
                if (fp.isNullOrBlank()) {
                    fp = "chrome"
                }
            }
            
            if (fp != null) {
                utls = SingBoxOptions.OutboundUTLSOptions().apply {
                    enabled = true
                    fingerprint = fp
                }
                // 🚀 终极真理：在 Sing-box 中开启指纹时，绝对、千万不能强加 ALPN，否则指纹破功被防火墙秒杀！
                alpn = null 
            } else {
                alpn = bean.alpn.blankAsNull()?.listByLineOrComma()
            }
            
            bean.echConfig.blankAsNull()?.let {
                ech = SingBoxOptions.OutboundECHOptions().apply {
                    enabled = true
                    config = if (it.contains("BEGIN ECH CONFIGS")) {
                        listOf(it)
                    } else {
                        listOf("-----BEGIN ECH CONFIGS-----", it.trim(), "-----END ECH CONFIGS-----")
                    }
                }
            }
        }
    }
}

fun AnyTLSBean.toUri(): String {
    val builder = linkBuilder()
        .host(serverAddress)
        .port(serverPort)
        .username(password)
    if (!name.isNullOrBlank()) {
        builder.encodedFragment(name.urlSafe())
    }
    if (allowInsecure) builder.addQueryParameter("insecure", "1")
    if (!sni.isNullOrBlank()) builder.addQueryParameter("sni", sni)
    if (!utlsFingerprint.isNullOrBlank()) builder.addQueryParameter("fp", utlsFingerprint)
    if (!realityPubKey.isNullOrBlank()) builder.addQueryParameter("pbk", realityPubKey)
    if (!realityShortId.isNullOrBlank()) builder.addQueryParameter("sid", realityShortId)
    
    minIdleSession?.let { builder.addQueryParameter("mis", it.toString()) }
    idleSessionCheckInterval?.let { builder.addQueryParameter("isci", it.toString()) }
    idleSessionTimeout?.let { builder.addQueryParameter("ist", it.toString()) }
    
    return builder.toLink("anytls")
}

fun parseAnytls(url: String): AnyTLSBean {
    val link = url.replace("anytls://", "https://").toHttpUrlOrNull() ?: error(
        "invalid anytls link $url"
    )
    return AnyTLSBean().apply {
        serverAddress = link.host
        serverPort = link.port
        name = link.fragment
        password = link.username
        sni = link.queryParameter("sni") ?: ""
        link.queryParameter("insecure")?.also {
            allowInsecure = it == "1" || it == "true"
        }
        link.queryParameter("fp")?.let { utlsFingerprint = it }
        link.queryParameter("pbk")?.let { realityPubKey = it }
        link.queryParameter("sid")?.let { realityShortId = it }
        
        link.queryParameter("mis")?.toIntOrNull()?.let { minIdleSession = it }
        link.queryParameter("isci")?.toIntOrNull()?.let { idleSessionCheckInterval = it }
        link.queryParameter("ist")?.toIntOrNull()?.let { idleSessionTimeout = it }
    }
}