package io.nekohasekai.sagernet.utils

/** Snapshot of the usable IPv4 addresses for Wi-Fi and hotspot interfaces. */
data class LanAddresses(
    val wifiIpv4: String? = null,
    val hotspotRouterIpv4: String? = null,
)
