package moe.matsuri.nb4a

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.net.LinkProperties
import libcore.InterfaceUpdateListener
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config
import org.json.JSONArray

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [28, 31], manifest = Config.NONE)
class AndroidNetworkMonitorTest {
    private class Listener(private val id: Long) : InterfaceUpdateListener {
        val indices = mutableListOf<Int>()
        override fun networkMonitorID() = id
        override fun updateDefaultInterface(name: String, index: Int, expensive: Boolean, constrained: Boolean) { indices.add(index) }
    }
    @Test fun callbackRegistrationCancellationAndMultipleBoxes() {
        val cm = RuntimeEnvironment.getApplication().getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager
        val shadow = shadowOf(cm)
        val monitor = AndroidNetworkMonitor(cm)
        val a = Listener(1); val b = Listener(2)
        val before = shadow.networkCallbacks.toSet()
        monitor.start(a); monitor.start(a); monitor.start(b)
        val callbacks = shadow.networkCallbacks.toSet() - before
        assertEquals(2, callbacks.size)
        val network = org.robolectric.shadows.ShadowNetwork.newInstance(123)
        val real = java.net.NetworkInterface.getNetworkInterfaces().toList().first { it.index > 0 }
        val properties = LinkProperties().apply { interfaceName = real.name }
        shadow.setLinkProperties(network, properties)
        val capabilities = NetworkCapabilities()
        shadowOf(capabilities).addTransportType(NetworkCapabilities.TRANSPORT_WIFI)
        shadowOf(capabilities).addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
        shadowOf(capabilities).addCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN)
        shadow.setNetworkCapabilities(network, capabilities)
        callbacks.forEach { it.onAvailable(network); it.onLinkPropertiesChanged(network, properties); it.onCapabilitiesChanged(network, capabilities); it.onLost(network) }
        assertEquals(listOf(-1, real.index, real.index, real.index, -1), a.indices)
        assertEquals(5, a.indices.size); assertEquals(5, b.indices.size)
        // A different Java proxy with the same Go identity must remove A only.
        monitor.close(Listener(1)); monitor.close(a)
        assertEquals(before.size + 1, shadow.networkCallbacks.size)
        callbacks.forEach { it.onAvailable(network) }
        assertEquals(5, a.indices.size); assertEquals(6, b.indices.size)
        monitor.close(b)
        assertEquals(before, shadow.networkCallbacks.toSet())
    }
    @Test fun initialSnapshotPrecedesDelayedCallbackAndRejectsVPN() {
        val cm = RuntimeEnvironment.getApplication().getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager
        val shadow = shadowOf(cm)
        val network = org.robolectric.shadows.ShadowNetwork.newInstance(987)
        val real = java.net.NetworkInterface.getNetworkInterfaces().toList().first { it.index > 0 }
        shadow.addNetwork(network, org.robolectric.shadows.ShadowNetworkInfo.newInstance(android.net.NetworkInfo.DetailedState.CONNECTED, 1, 0, true, true))
        shadow.setLinkProperties(network, LinkProperties().apply { interfaceName = real.name })
        val caps = NetworkCapabilities()
        shadowOf(caps).addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
        shadowOf(caps).addCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN)
        shadowOf(caps).addTransportType(NetworkCapabilities.TRANSPORT_WIFI)
        shadow.setNetworkCapabilities(network, caps)
        if (android.os.Build.VERSION.SDK_INT >= 23) shadow.setDefaultNetworkActive(true)
        val monitor = AndroidNetworkMonitor(cm)
        val listener = Listener(987)
        monitor.start(listener)
        assertEquals("start must synchronously publish a snapshot (offline until the authoritative callback on Robolectric)", listOf(-1), listener.indices)
        val startupCallback = (shadow.networkCallbacks.toSet()).single()
        startupCallback.onAvailable(network)
        assertEquals(listOf(-1, real.index), listener.indices)
        monitor.close(listener)
        shadowOf(caps).addTransportType(NetworkCapabilities.TRANSPORT_VPN)
        shadow.setNetworkCapabilities(network, caps)
        val vpnListener = Listener(988)
        monitor.start(vpnListener)
        assertFalse(vpnListener.indices.contains(real.index))
        val interfaces = JSONArray(monitor.interfacesJSON())
        assertFalse((0 until interfaces.length()).any { interfaces.getJSONObject(it).getString("Name") == real.name })
        monitor.close(vpnListener)
    }
    @Test fun networkSelectionTraceIsBoundedPrivateAndNonZero() {
        val cm = RuntimeEnvironment.getApplication().getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager
        val shadow = shadowOf(cm)
        val network = org.robolectric.shadows.ShadowNetwork.newInstance(2468)
        val real = java.net.NetworkInterface.getNetworkInterfaces().toList().first { it.index > 0 }
        val properties = LinkProperties().apply { interfaceName = real.name }
        val capabilities = NetworkCapabilities()
        shadowOf(capabilities).addTransportType(NetworkCapabilities.TRANSPORT_CELLULAR)
        shadowOf(capabilities).addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
        shadowOf(capabilities).addCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN)
        shadowOf(capabilities).addCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED)
        shadow.setLinkProperties(network, properties)
        shadow.setNetworkCapabilities(network, capabilities)
        val traces = mutableListOf<String>()
        val monitor = AndroidNetworkMonitor({ cm }) { traces.add(it) }
        val listener = Listener(2468)
        monitor.start(listener)
        (shadow.networkCallbacks.toSet()).single().onAvailable(network)
        val line = traces.last()
        for (field in listOf("schema=4", "seq=", "active=", "selected=", "underlying=", "interface=", "transport=1", "default=", "validated=true")) {
            assertTrue("missing $field: $line", line.contains(field))
        }
        assertFalse(line.contains(real.name))
        assertFalse(line.contains("2468"))
        assertFalse(line.contains("active=0 selected=0"))
        monitor.close(listener)
    }
    @Test fun interfaceEnumerationUsesActualJavaInterfaces() {
        val cm = RuntimeEnvironment.getApplication().getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager
        val interfaces = JSONArray(AndroidNetworkMonitor(cm).interfacesJSON())
        assertTrue(interfaces.length() > 0)
        for (i in 0 until interfaces.length()) {
            val entry = interfaces.getJSONObject(i)
            assertTrue(entry.getInt("Index") > 0)
            assertTrue(entry.getString("Name").isNotEmpty())
            assertNotNull(entry.getJSONArray("Addresses"))
        }
    }
}
