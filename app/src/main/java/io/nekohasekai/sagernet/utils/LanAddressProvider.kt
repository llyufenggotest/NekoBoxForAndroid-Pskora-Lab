package io.nekohasekai.sagernet.utils

import android.content.Context
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import java.net.Inet4Address
import java.net.NetworkInterface

/** Finds Wi-Fi and hotspot IPv4 addresses without exposing loopback/VPN addresses. */
object LanAddressProvider {
    fun current(context: Context): LanAddresses {
        val cm = context.getSystemService(Context.CONNECTIVITY_SERVICE) as? ConnectivityManager
        val wifi = mutableSetOf<String>()
        cm?.allNetworks?.forEach { network ->
            val caps = cm.getNetworkCapabilities(network) ?: return@forEach
            if (!caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) || caps.hasTransport(NetworkCapabilities.TRANSPORT_VPN)) return@forEach
            cm.getLinkProperties(network)?.linkAddresses?.forEach { link ->
                (link.address as? Inet4Address)?.hostAddress?.let(wifi::add)
            }
        }
        var hotspot: String? = null
        try {
            val interfaces = NetworkInterface.getNetworkInterfaces()
            while (interfaces.hasMoreElements()) {
                val nif = interfaces.nextElement()
                val name = nif.name?.lowercase() ?: continue
                val isHotspot = name.startsWith("ap") || name.startsWith("wlan") || name.startsWith("swlan") || name.contains("softap")
                if (!isHotspot) continue
                val addresses = nif.inetAddresses
                while (addresses.hasMoreElements()) {
                    val address = addresses.nextElement() as? Inet4Address ?: continue
                    if (!address.isLoopbackAddress && !address.isLinkLocalAddress) {
                        hotspot = address.hostAddress
                        break
                    }
                }
            }
        } catch (_: Exception) {
            // Address discovery is best effort; the UI reports unavailable.
        }
        return LanAddresses(wifi.firstOrNull(), hotspot)
    }
}
