package io.nekohasekai.sagernet.ui.profile

import android.app.Application
import android.os.Bundle
import androidx.fragment.app.FragmentActivity
import androidx.preference.EditTextPreference
import androidx.preference.PreferenceFragmentCompat
import androidx.preference.SwitchPreference
import io.nekohasekai.sagernet.R
import moe.matsuri.nb4a.ui.SimpleMenuPreference
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [28, 31], application = Application::class)
class GostPluginSettingsUiTest {
    @Test fun structuredStateRoundTripsAndPreservesAdvancedUnknownOptions() {
        val raw = "mode=mwss;host=cdn.invalid;path=/gost;mux=3;insecure;nocomp;future=value\\;semi"
        val state = GostPluginUiState.fromPlugin("gost-plugin;$raw")
        assertEquals("mws", state.mode)
        assertTrue(state.tls)
        assertEquals("cdn.invalid", state.host)
        assertEquals("/gost", state.path)
        assertEquals("3", state.mux)
        assertEquals(raw, state.rawOptions)

        val saved = state.copy(mode = "ws", tls = false, host = "edge.invalid", path = "/next")
            .toPlugin("gost-plugin")
        assertEquals(
            "gost-plugin;mode=ws;host=edge.invalid;path=/next;insecure;nocomp;mux=3;future=value\\;semi",
            saved,
        )
    }

    @Test fun ordinaryPluginRemainsOpaqueAndUnchanged() {
        val raw = "mode=websocket;host=cdn.invalid;path=/v;mux=8"
        val state = GostPluginUiState.fromPlugin("v2ray-plugin;$raw")
        assertFalse(state.enabled)
        assertEquals("v2ray-plugin;$raw", state.toPlugin("v2ray-plugin"))
    }

    @Test fun preferencesAreStructuredOnlyForGostAndSurviveFragmentRecreation() {
        val controller = Robolectric.buildActivity(FragmentActivity::class.java)
        val activity = controller.get().apply { setTheme(R.style.Theme_SagerNet) }
        controller.setup()
        var fragment = Harness()
        activity.supportFragmentManager.beginTransaction().add(fragment, "settings").commitNow()

        fragment.plugin.value = "gost-plugin"
        configureGostPluginPreferences(fragment, "gost-plugin")
        assertTrue(fragment.mode.isVisible)
        assertTrue(fragment.host.isVisible)
        assertTrue(fragment.path.isVisible)
        assertTrue(fragment.tls.isVisible)
        assertTrue(fragment.mux.isVisible)
        assertTrue(fragment.raw.isVisible)
        fragment.mode.value = "mws"
        fragment.host.text = "cdn.invalid"
        fragment.path.text = "/gost"
        fragment.tls.isChecked = true
        fragment.mux.text = "3"
        fragment.raw.text = "mode=mwss;host=cdn.invalid;path=/gost;mux=3;future=value\\;semi"

        activity.supportFragmentManager.beginTransaction().remove(fragment).commitNow()
        fragment = Harness()
        activity.supportFragmentManager.beginTransaction().add(fragment, "settings2").commitNow()
        assertEquals("mws", fragment.mode.value)
        assertEquals("cdn.invalid", fragment.host.text)
        assertEquals("/gost", fragment.path.text)
        assertTrue(fragment.tls.isChecked)
        assertEquals("3", fragment.mux.text)
        assertTrue(fragment.raw.text!!.contains("future=value\\;semi"))

        fragment.plugin.value = "obfs-local"
        configureGostPluginPreferences(fragment, "obfs-local")
        assertFalse(fragment.mode.isVisible)
        assertFalse(fragment.host.isVisible)
        assertFalse(fragment.path.isVisible)
        assertFalse(fragment.tls.isVisible)
        assertFalse(fragment.mux.isVisible)
        assertTrue(fragment.raw.isVisible)
        assertTrue(fragment.raw.text!!.contains("future=value\\;semi"))
    }

    class Harness : PreferenceFragmentCompat() {
        lateinit var plugin: SimpleMenuPreference
        lateinit var raw: EditTextPreference
        lateinit var mode: SimpleMenuPreference
        lateinit var host: EditTextPreference
        lateinit var path: EditTextPreference
        lateinit var tls: SwitchPreference
        lateinit var mux: EditTextPreference

        override fun onCreatePreferences(savedInstanceState: Bundle?, rootKey: String?) {
            preferenceManager.sharedPreferencesName = "gost-plugin-ui-test"
            addPreferencesFromResource(R.xml.shadowsocks_preferences)
            plugin = findPreference("pluginName")!!
            raw = findPreference("pluginConfig")!!
            mode = findPreference("gostPluginMode")!!
            host = findPreference("gostPluginHost")!!
            path = findPreference("gostPluginPath")!!
            tls = findPreference("gostPluginTls")!!
            mux = findPreference("gostPluginMux")!!
        }
    }
}
