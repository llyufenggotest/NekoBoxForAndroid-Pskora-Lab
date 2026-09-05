package io.nekohasekai.sagernet.utils

import android.content.Context
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import java.net.Inet4Address
import java.net.NetworkInterface

/** Selects distinct active Wi-Fi and tethering IPv4 addresses. */
object LanAddressProvider {
    fun current(context: Context): LanAddresses {
        val cm = context.getSystemService(Context.CONNECTIVITY_SERVICE) as? ConnectivityManager
        val activeWifi = linkedSetOf<String>()
        cm?.allNetworks?.forEach { network ->
            val caps = cm.getNetworkCapabilities(network) ?: return@forEach
            if (!caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) ||
                caps.hasTransport(NetworkCapabilities.TRANSPORT_VPN)
            ) return@forEach
            cm.getLinkProperties(network)?.linkAddresses?.forEach { link ->
                val address = link.address as? Inet4Address ?: return@forEach
                if (!address.isLoopbackAddress && !address.isLinkLocalAddress) {
                    address.hostAddress?.let(activeWifi::add)
                }
            }
        }

        val hotspotCandidates = mutableListOf<Pair<Int, String>>()
        try {
            val interfaces = NetworkInterface.getNetworkInterfaces()
            while (interfaces.hasMoreElements()) {
                val networkInterface = interfaces.nextElement()
                if (!networkInterface.isUp || networkInterface.isLoopback) continue
                val name = networkInterface.name?.lowercase() ?: continue
                val rank = hotspotRank(name)
                if (rank == Int.MAX_VALUE) continue
                val addresses = networkInterface.inetAddresses
                while (addresses.hasMoreElements()) {
                    val address = addresses.nextElement() as? Inet4Address ?: continue
                    val host = address.hostAddress ?: continue
                    if (address.isLoopbackAddress || address.isLinkLocalAddress || host in activeWifi) continue
                    hotspotCandidates += rank to host
                }
            }
        } catch (_: Exception) {
            // Best effort: unavailable is safer than advertising a wrong address.
        }

        return LanAddresses(
            wifiIpv4 = activeWifi.firstOrNull(),
            hotspotRouterIpv4 = hotspotCandidates.minByOrNull { it.first }?.second,
        )
    }

    private fun hotspotRank(name: String): Int = when {
        name.startsWith("ap") || name.startsWith("swlan") || name.contains("softap") -> 0
        name.startsWith("wlan") -> 1
        else -> Int.MAX_VALUE
    }
}
