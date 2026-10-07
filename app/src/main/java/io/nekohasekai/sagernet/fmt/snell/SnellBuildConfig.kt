package io.nekohasekai.sagernet.fmt.snell

import moe.matsuri.nb4a.SingBoxOptions

fun buildSingBoxOutboundSnellBean(bean: SnellBean): SingBoxOptions.Outbound_SnellOptions {
    return SingBoxOptions.Outbound_SnellOptions().apply {
        type = "snell"
        server = bean.serverAddress
        server_port = bean.serverPort
        psk = bean.psk
        tcp_fast_open = bean.tcpFastOpen
        if (!bean.userKey.isNullOrBlank()) {
            userkey = bean.userKey
        }
        version = bean.version

        if (bean.network != null && bean.network.isNotBlank()) {
            network = bean.network
        }

        if (bean.version == 6) {
            if (!bean.mode.isNullOrBlank() && bean.mode != "default") {
                mode = bean.mode
            }
            if (bean.quicProxyMode == true) {
                quic_proxy_mode = true
            }
        } else if (bean.oixEchTls == true) {
            // Preserve the OIX marker and metadata in the generated JSON only.
            // The underlying OIX transport is not implemented by this layer.
            version = 4
            obfs_mode = "oix-ech-tls"
            oix_ech = true
            oix_identity_version = bean.oixIdentityVersion
            oix_alpn = bean.oixAlpn
            oix_legacy_fallback = bean.oixLegacyFallback
            oix_preconnect = bean.oixPreconnect
            oix_sni = bean.oixSni
            oix_config = bean.oixConfig
            // Absence must retain native exporter identity, never become explicit false.
            oix_identity = bean.identity ?: true
            oix_path = bean.oixPath
            oix_skip_cert_verify = bean.oixSkipCertVerify
        } else if (bean.obfsMode != null && bean.obfsMode.isNotBlank() && !isOixEchTlsMode(bean.obfsMode)) {
            obfs_mode = if (bean.version != null && bean.version >= 4 && bean.obfsMode == "tls") "" else bean.obfsMode
            if (obfs_mode.isNotBlank() && bean.obfsHost != null && bean.obfsHost.isNotBlank()) {
                obfs_host = bean.obfsHost
            }
        }

        if (bean.reuse != null && bean.reuse) {
            this.reuse = true
        }
    }
}
