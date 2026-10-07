package io.nekohasekai.sagernet.fmt.snell

import io.nekohasekai.sagernet.ktx.urlSafe

import okhttp3.HttpUrl.Companion.toHttpUrlOrNull

// URI values are UTF-8 percent-encoded, not base64. HttpUrl decodes once.
fun parseSnell(url: String): SnellBean {
    val link = url.replace("snell://", "https://").toHttpUrlOrNull()
        ?: error("Invalid snell URL")

    return SnellBean().apply {
        serverAddress = link.host
        serverPort = link.port
        psk = link.username
        name = link.fragment ?: ""

        link.queryParameter("version")?.toIntOrNull()?.let {
            version = it.coerceIn(1, 6)
        }
        link.queryParameter("userkey")?.let { userKey = it }
        link.queryParameter("obfs-mode")?.let { obfsMode = it }
        link.queryParameter("obfs-host")?.let { obfsHost = it }
        link.queryParameter("reuse")?.let { reuse = it.toBoolean() }
        link.queryParameter("network")?.let { network = it }
        link.queryParameter("mode")?.let { mode = it }
        link.queryParameter("quic-proxy-mode")?.let { quicProxyMode = it.toBoolean() }
        link.queryParameter("oix-ech")?.let { oixEchTls = it.toBoolean() }
        link.queryParameter("oix-identity-version")?.toIntOrNull()?.let { oixIdentityVersion = it }
        link.queryParameter("oix-alpn")?.let { oixAlpn = it }
        link.queryParameter("oix-legacy-fallback")?.let { oixLegacyFallback = it.toBoolean() }
        link.queryParameter("oix-preconnect")?.toIntOrNull()?.let { oixPreconnect = it }
        link.queryParameter("oix-sni")?.let { oixSni = it }
        link.queryParameter("oix-config")?.let { oixConfig = it }
        link.queryParameter("identity")?.let { identity = it.toBoolean(); identityPresent = true }
        link.queryParameter("oix-path")?.let { oixPath = it }
        link.queryParameter("oix-skip-cert-verify")?.let { oixSkipCertVerify = it.toBoolean() }
        link.queryParameter("tfo")?.let { tcpFastOpen = it.toBoolean() }
        initializeDefaultValues()
    }
}

fun SnellBean.toUri(): String {
    val builder = StringBuilder("snell://")
    builder.append(psk.urlSafe()).append("@")
    builder.append(if (serverAddress.contains(":")) "[$serverAddress]" else serverAddress).append(":").append(serverPort)

    val params = mutableListOf<String>()
    params.add("version=$version")
    if (userKey.isNotBlank()) params.add("userkey=${userKey.urlSafe()}")
    if (version == 6) {
        if (mode.isNotBlank() && mode != "default") params.add("mode=${mode.urlSafe()}")
        if (quicProxyMode == true) params.add("quic-proxy-mode=true")
    } else {
        if (obfsMode.isNotBlank()) params.add("obfs-mode=${obfsMode.urlSafe()}")
        if (obfsHost.isNotBlank()) params.add("obfs-host=${obfsHost.urlSafe()}")
    }
    // Retain metadata even with Oix disabled: sharing must not erase editor values.
    if (oixEchTls == true || oixConfig.isNotBlank() || oixSni.isNotBlank() || oixPath.isNotBlank() || oixSkipCertVerify == true || oixIdentityVersion != 2 || oixAlpn != "snell-ech/1" || oixLegacyFallback == true || oixPreconnect != 0) {
        params.add("oix-ech=$oixEchTls")
        params.add("oix-identity-version=$oixIdentityVersion")
        params.add("oix-alpn=${oixAlpn.urlSafe()}")
        params.add("oix-legacy-fallback=$oixLegacyFallback")
        params.add("oix-preconnect=$oixPreconnect")
        params.add("oix-sni=${oixSni.urlSafe()}")
        params.add("oix-config=${oixConfig.urlSafe()}")
    }
    if (identityPresent || identity == false) params.add("identity=$identity")
    if (oixPath.isNotBlank()) params.add("oix-path=${oixPath.urlSafe()}")
    if (oixSkipCertVerify == true) params.add("oix-skip-cert-verify=true")
    params.add("tfo=$tcpFastOpen")
    if (reuse) params.add("reuse=true")
    if (network.isNotBlank()) params.add("network=${network.urlSafe()}")

    builder.append("?").append(params.joinToString("&"))

    if (name.isNotBlank()) {
        builder.append("#").append(name.urlSafe())
    }

    return builder.toString()
}

fun parseClashSnell(proxy: Map<String, Any?>): SnellBean {
    return SnellBean().apply {
        name = proxy["name"] as? String ?: ""
        serverAddress = proxy["server"] as? String ?: ""
        serverPort = proxy["port"].toString().toIntOrNull() ?: 443
        psk = proxy["psk"] as? String ?: ""
        proxy["identity"]?.let { identity = it.toString().toBoolean(); identityPresent = true }
        tcpFastOpen = proxy["tfo"].toString().toBoolean()

        version = (proxy["version"].toString().toIntOrNull() ?: 4).coerceIn(1, 6)
        userKey = proxy["userkey"] as? String ?: ""
        mode = proxy["mode"] as? String ?: "default"
        quicProxyMode = proxy["quic-proxy-mode"].toString().toBoolean()

        reuse = proxy["reuse"].toString().toBoolean()

        val udpEnabled = proxy["udp"].toString().toBoolean()
        network = if (udpEnabled) {
            ""
        } else {
            "tcp"
        }

        // obfs-opts
        (proxy["obfs-opts"] as? Map<*, *>)?.let { obfsOpts ->
            obfsMode = obfsOpts["mode"] as? String ?: ""
            obfsHost = obfsOpts["host"] as? String ?: ""
            if (isOixEchTlsMode(obfsMode)) {
                // Oix is independent of ordinary Snell obfuscation. Never retain its
                // YAML marker as a normal obfs mode when the Oix switch is disabled.
                obfsMode = ""
                oixEchTls = true
                oixIdentityVersion = obfsOpts["identity-version"].toString().toIntOrNull() ?: 2
                oixAlpn = obfsOpts["alpn"] as? String ?: "snell-ech/1"
                oixLegacyFallback = obfsOpts["legacy-fallback"].toString().toBoolean()
                oixPreconnect = obfsOpts["preconnect"].toString().toIntOrNull() ?: 0
                oixSni = obfsOpts["sni"] as? String ?: ""
                oixConfig = obfsOpts["ech-config"] as? String ?: ""
                oixPath = obfsOpts["path"] as? String ?: ""
                oixSkipCertVerify = obfsOpts["skip-cert-verify"].toString().toBoolean()
            }
        }
        initializeDefaultValues()
    }
}

internal fun isOixEchTlsMode(mode: String?): Boolean = mode == "ech-tls" || mode == "oix-ech-tls"
