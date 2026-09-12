package moe.matsuri.nb4a

import android.annotation.SuppressLint
import android.net.ConnectivityManager
import android.net.LinkProperties
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import android.os.Build
import android.os.Handler
import android.os.Looper
import io.nekohasekai.sagernet.SagerNet
import libcore.InterfaceUpdateListener
import android.util.Log
import org.json.JSONArray
import org.json.JSONObject
import java.net.NetworkInterface
import java.util.concurrent.atomic.AtomicLong

/** Each native box owns a registration; tests and the VPN can coexist. */
internal class AndroidNetworkMonitor(
    managerProvider: () -> ConnectivityManager = { SagerNet.connectivity },
    private val trace: (String) -> Unit = { Log.d("NETSELTRACE", it) }
) {
    constructor() : this({ SagerNet.connectivity })
    constructor(manager: ConnectivityManager) : this({ manager })

    // NativeInterface is constructed by Application before attachBaseContext.
    // Resolve the real service only when native networking starts/enumerates after attach.
    private val manager: ConnectivityManager by lazy(managerProvider)
    private val registrations = mutableMapOf<Long, ConnectivityManager.NetworkCallback>()
    private val traceSequence = AtomicLong()
    @Volatile private var authoritativeSelectedHandle: Long = 0L

    private fun stableHash(value: Long): Long {
        var x = value
        x = (x xor (x ushr 33)) * -49064778989728563L
        x = (x xor (x ushr 33)) * -4265267296055464877L
        return (x xor (x ushr 33)) and Long.MAX_VALUE
    }

    private fun stableHash(value: String?): Long {
        if (value == null) return 0
        var hash = -3750763034362895579L
        value.forEach { hash = (hash xor it.code.toLong()) * 1099511628211L }
        return hash and Long.MAX_VALUE
    }

    private fun transportCode(caps: NetworkCapabilities?): Int = when {
        caps?.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR) == true -> 1
        caps?.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) == true -> 2
        caps?.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET) == true -> 3
        caps?.hasTransport(NetworkCapabilities.TRANSPORT_VPN) == true -> 4
        else -> 0
    }

    @SuppressLint("NewApi")
    @Synchronized
    fun start(listener: InterfaceUpdateListener) {
        val id = listener.networkMonitorID()
        if (registrations.containsKey(id)) return
        val callback = object : ConnectivityManager.NetworkCallback() {
            private var current: Network? = null
            private fun isUsable(network: Network): Boolean {
                val caps = manager.getNetworkCapabilities(network) ?: return false
                val lp = manager.getLinkProperties(network) ?: return false
                return !caps.hasTransport(NetworkCapabilities.TRANSPORT_VPN) &&
                    !lp.interfaceName.isNullOrEmpty()
            }
            private fun activePhysical(candidate: Network?): Network? {
                val active = if (Build.VERSION.SDK_INT >= 23) manager.activeNetwork else null
                // The registered callback itself is constrained to NOT_VPN. On devices
                // where activeNetwork is unavailable, it is therefore authoritative.
                if (active == null) return candidate
                // activeNetwork can be this app's VPN. In that case prefer its declared
                // underlying handles; never fall back to callback order or allNetworks order.
                val activeCapabilities = manager.getNetworkCapabilities(active)
                val activeIsVpn = activeCapabilities?.hasTransport(NetworkCapabilities.TRANSPORT_VPN) == true
                // Android SDK 35 exposes no public getter for a VPN's underlying
                // Network handles. Use this monitor's authoritative NOT_VPN
                // default/best-match callback when activeNetwork is the app VPN;
                // never infer a physical default from allNetworks ordering.
                val callbackFallback = if (activeIsVpn || !isUsable(active)) candidate else null
                return PhysicalDefaultNetworkSelector.select(active, callbackFallback, emptyList(), ::isUsable)
            }
            private fun update(candidate: Network?) {
                synchronized(this@AndroidNetworkMonitor) {
                    if (registrations[id] !== this) return
                    val network = activePhysical(candidate)
                    val lp = network?.let { manager.getLinkProperties(it) }
                    val caps = network?.let { manager.getNetworkCapabilities(it) }
                    val name = lp?.interfaceName
                    val intf = name?.let { NetworkInterface.getByName(it) }
                    val active = if (Build.VERSION.SDK_INT >= 23) manager.activeNetwork else null
                    val activeCaps = active?.let { manager.getNetworkCapabilities(it) }
                    val activeIsVpn = activeCaps?.hasTransport(NetworkCapabilities.TRANSPORT_VPN) == true
                    val activeHash = active?.let { stableHash(it.networkHandle) } ?: 0
                    val selectedHash = network?.let { stableHash(it.networkHandle) } ?: 0
                    val underlyingHash = if (activeIsVpn) selectedHash else 0
                    authoritativeSelectedHandle = network?.networkHandle ?: 0L
                    trace("NETSELTRACE schema=4 seq=${traceSequence.incrementAndGet()} active=$activeHash selected=$selectedHash underlying=$underlyingHash interface=${stableHash(name)} transport=${transportCode(caps)} default=${active == network || activeIsVpn} validated=${caps?.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED) == true}")
                    listener.updateDefaultInterface(name ?: "", intf?.index ?: -1,
                        caps != null && !caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_METERED),
                        Build.VERSION.SDK_INT >= 28 && caps != null && !caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_CONGESTED))
                }
            }
            override fun onAvailable(network: Network) = update(network)
            override fun onLinkPropertiesChanged(network: Network, properties: LinkProperties) = update(network)
            override fun onCapabilitiesChanged(network: Network, capabilities: NetworkCapabilities) = update(network)
            override fun onLost(network: Network) = update(null)
        }
        registrations[id] = callback
        val request = NetworkRequest.Builder()
            .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
            .addCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN)
            .addCapability(NetworkCapabilities.NET_CAPABILITY_NOT_RESTRICTED).build()
        try {
            val handler = Handler(Looper.getMainLooper())
            // Same underlying-network selection as DefaultNetworkListener, not a VPN default.
            when {
                Build.VERSION.SDK_INT >= 31 -> manager.registerBestMatchingNetworkCallback(request, callback, handler)
                Build.VERSION.SDK_INT >= 28 -> manager.requestNetwork(request, callback, handler)
                Build.VERSION.SDK_INT >= 26 -> manager.registerDefaultNetworkCallback(callback, handler)
                Build.VERSION.SDK_INT >= 24 -> manager.registerDefaultNetworkCallback(callback)
                else -> manager.requestNetwork(request, callback)
            }
            // Registration only queues callbacks. Resolve through activeNetwork (or
            // the active VPN's underlying handles) so array order cannot select IMS.
            manager.activeNetwork?.let { callback.onAvailable(it) }
        } catch (error: Exception) { registrations.remove(id); runCatching { manager.unregisterNetworkCallback(callback) }; throw error }
    }

    @Synchronized
    fun close(listener: InterfaceUpdateListener) {
        val callback = registrations.remove(listener.networkMonitorID()) ?: return
        manager.unregisterNetworkCallback(callback)
    }

    fun interfacesJSON(): String {
        val result = JSONArray()
        val networks = manager.allNetworks.mapNotNull { network ->
            val lp = manager.getLinkProperties(network) ?: return@mapNotNull null
            network to Pair(lp, manager.getNetworkCapabilities(network))
        }
        val interfaces = NetworkInterface.getNetworkInterfaces() ?: throw IllegalStateException("Android interface enumeration unavailable")
        while (interfaces.hasMoreElements()) {
            val intf = interfaces.nextElement()
            // Android VPNs inherit cellular/Wi-Fi capabilities. They must never
            // become candidates for the physical interface racing dialer.
            if (networks.any { it.second.first.interfaceName == intf.name && it.second.second?.hasTransport(NetworkCapabilities.TRANSPORT_VPN) == true }) continue
            val details = networks.firstOrNull { it.second.first.interfaceName == intf.name }
            val selectedNetwork = details?.first
            val lp = details?.second?.first
            val caps = details?.second?.second
            var flags = 0
            if (intf.isUp) flags = flags or 1 or 32 // net.FlagUp | net.FlagRunning
            if (intf.isLoopback) flags = flags or 4
            if (intf.isPointToPoint) flags = flags or 8
            if (intf.supportsMulticast()) flags = flags or 16
            val addresses = JSONArray()
            for (address in intf.interfaceAddresses) {
                addresses.put("${address.address.hostAddress!!.substringBefore('%')}/${address.networkPrefixLength}")
            }
            val dns = JSONArray()
            lp?.dnsServers?.forEach { dns.put(it.hostAddress!!.substringBefore('%')) }
            val gateways = JSONArray()
            lp?.routes?.mapNotNull { it.gateway }?.filterNot { it.isAnyLocalAddress }?.forEach { gateways.put(it.hostAddress!!.substringBefore('%')) }
            val type = when {
                caps?.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) == true -> 0
                caps?.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR) == true -> 1
                caps?.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET) == true -> 2
                else -> 3
            }
            val isAuthoritative = selectedNetwork?.networkHandle == authoritativeSelectedHandle && authoritativeSelectedHandle != 0L
            val defaultRoute = lp?.routes?.any { route -> route.isDefaultRoute && route.hasGateway() } == true
            val canonicalComplete = isAuthoritative && lp != null && caps != null &&
                intf.index > 0 && intf.name.isNotEmpty() && intf.interfaceAddresses.toList().isNotEmpty() &&
                lp.dnsServers.isNotEmpty() && defaultRoute
            result.put(JSONObject().put("Name", intf.name).put("Index", intf.index)
                .put("MTU", intf.mtu).put("Flags", flags).put("Addresses", addresses)
                .put("Type", type).put("DNSServers", dns).put("Gateways", gateways)
                .put("Expensive", caps != null && !caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_METERED))
                .put("Constrained", Build.VERSION.SDK_INT >= 28 && caps != null && !caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_CONGESTED))
                .put("SelectedIdentity", selectedNetwork?.let { stableHash(it.networkHandle) } ?: 0L)
                .put("PhysicalDefault", isAuthoritative && caps != null && !caps.hasTransport(NetworkCapabilities.TRANSPORT_VPN))
                .put("Internet", caps?.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET) == true)
                .put("Validated", caps?.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED) == true)
                .put("DefaultRoute", defaultRoute)
                .put("CanonicalComplete", canonicalComplete))
        }
        return result.toString()
    }
}
