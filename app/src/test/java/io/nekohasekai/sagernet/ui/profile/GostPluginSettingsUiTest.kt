package io.nekohasekai.sagernet.ui.profile

import android.app.Application
import android.os.Bundle
import androidx.fragment.app.FragmentActivity
import androidx.preference.EditTextPreference
import androidx.preference.PreferenceFragmentCompat
import androidx.preference.SwitchPreference
import io.nekohasekai.sagernet.R
import io.nekohasekai.sagernet.RoomLifecycleRule
import io.nekohasekai.sagernet.database.DataStore
import io.nekohasekai.sagernet.database.ProxyGroup
import io.nekohasekai.sagernet.database.SagerDatabase
import io.nekohasekai.sagernet.fmt.shadowsocks.ShadowsocksBean
import kotlinx.coroutines.runBlocking
import moe.matsuri.nb4a.ui.SimpleMenuPreference
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [28, 31], application = Application::class)
class GostPluginSettingsUiTest {
    @get:Rule
    val roomLifecycle = RoomLifecycleRule()

    @Test fun structuredStateRoundTripsAndPreservesAdvancedUnknownOptions() {
        val raw = "mode=mwss;host=cdn.invalid;path=/gost;mux=3;insecure;nocomp;future=value\\;semi"
        val state = GostPluginUiState.fromPlugin("gost-plugin;$raw")
        assertEquals("mws", state.mode)
        assertTrue(state.tls)
        assertEquals("cdn.invalid", state.host)
        assertEquals("/gost", state.path)
        assertEquals("3", state.mux.editorText())
        assertEquals(raw, state.rawOptions)

        val saved = state.copy(mode = "ws", tls = false, host = "edge.invalid", path = "/next")
            .toPlugin("gost-plugin")
        assertEquals(
            "gost-plugin;mode=ws;host=edge.invalid;path=/next;insecure;nocomp;mux=3;future=value\\;semi",
            saved,
        )
    }

    @Test fun unsupportedModeAndMuxFlagRemainUntouchedUntilAnExplicitStructuredEdit() {
        val customRaw = "mode=grpc;host=cdn.invalid;mux;future=false"
        val custom = GostPluginUiState.fromPlugin("gost-plugin;$customRaw")
        assertEquals("grpc", custom.mode)
        assertFalse(custom.modeSupported)
        assertEquals(GostMuxValue.Flag, custom.mux)
        assertEquals("gost-plugin;$customRaw", custom.toPlugin("gost-plugin"))

        val explicit = custom.copy(mode = "ws", modeSupported = true, tls = true)
        assertEquals(
            "gost-plugin;mode=wss;host=cdn.invalid;mux;future=false",
            explicit.toPlugin("gost-plugin"),
        )
    }

    @Test fun ordinaryPluginRemainsOpaqueAndUnchanged() {
        val raw = "mode=websocket;host=cdn.invalid;path=/v;mux=8"
        val state = GostPluginUiState.fromPlugin("v2ray-plugin;$raw")
        assertFalse(state.enabled)
        assertEquals("v2ray-plugin;$raw", state.toPlugin("v2ray-plugin"))
    }

    @Test fun realActivityKeepsGostAndOtherPluginConfigsIsolatedAndMergesImmediately() {
        DataStore.editingId = 0L
        DataStore.profileCacheStore.putString("pluginName", "obfs-local")
        DataStore.profileCacheStore.putString("pluginConfig", "obfs=http;obfs-host=plain.invalid")
        val controller = Robolectric.buildActivity(ShadowsocksSettingsActivity::class.java).setup()
        val activity = controller.get()
        val fragment = activity.supportFragmentManager.fragments.single() as PreferenceFragmentCompat
        val plugin = fragment.findPreference<SimpleMenuPreference>("pluginName")!!
        val raw = fragment.findPreference<EditTextPreference>("pluginConfig")!!
        val host = fragment.findPreference<EditTextPreference>("gostPluginHost")!!

        plugin.callChangeListener("obfs-local")
        plugin.value = "obfs-local"
        raw.callChangeListener("obfs=http;obfs-host=plain.invalid")
        raw.text = "obfs=http;obfs-host=plain.invalid"
        plugin.callChangeListener("gost-plugin")
        plugin.value = "gost-plugin"
        raw.callChangeListener("mode=grpc;mux;future=false")
        raw.text = "mode=grpc;mux;future=false"
        host.callChangeListener("cdn.invalid")
        host.text = "cdn.invalid"
        assertEquals("mode=grpc;host=cdn.invalid;mux;future=false", raw.text)

        plugin.callChangeListener("obfs-local")
        plugin.value = "obfs-local"
        assertEquals("obfs=http;obfs-host=plain.invalid", raw.text)
        plugin.callChangeListener("gost-plugin")
        plugin.value = "gost-plugin"
        assertEquals("mode=grpc;host=cdn.invalid;mux;future=false", raw.text)
        controller.pause().stop().destroy()
    }

    @Test fun activityRecreationRestoresGostRawAndIsolatedPluginConfigsBeforeSerialize() {
        DataStore.profileCacheStore.reset()
        val controller = launchEditor()
        selectPlugin(controller.get(), "obfs-local")
        changeRaw(controller.get(), "obfs=http;obfs-host=plain.invalid")
        selectPlugin(controller.get(), "gost-plugin")
        changeRaw(controller.get(), "mode=grpc;mux;future=false")
        changeText(controller.get(), "gostPluginHost", "cdn.invalid")

        controller.recreate()
        assertEditorState(controller.get(), "gost-plugin", "mode=grpc;host=cdn.invalid;mux;future=false")
        selectPlugin(controller.get(), "obfs-local")
        assertEditorState(controller.get(), "obfs-local", "obfs=http;obfs-host=plain.invalid")
        selectPlugin(controller.get(), "gost-plugin")

        assertEquals(
            "gost-plugin;mode=grpc;host=cdn.invalid;mux;future=false",
            serializeProduction(controller.get()).plugin,
        )
        controller.pause().stop().destroy()
    }

    @Test fun processLikeActivityRestoreUsesCacheAsGostSourceOfTruth() {
        DataStore.profileCacheStore.reset()
        var controller = launchEditor()
        selectPlugin(controller.get(), "gost-plugin")
        changeRaw(controller.get(), "mode=mwss;host=cdn.invalid;path=/gost;mux=3;future=value\\;semi")
        controller.pause().stop().destroy()

        controller = Robolectric.buildActivity(ShadowsocksSettingsActivity::class.java).setup()
        assertEditorState(
            controller.get(),
            "gost-plugin",
            "mode=mwss;host=cdn.invalid;path=/gost;mux=3;future=value\\;semi",
        )
        assertEquals(
            "gost-plugin;mode=mwss;host=cdn.invalid;path=/gost;mux=3;future=value\\;semi",
            serializeProduction(controller.get()).plugin,
        )
        controller.pause().stop().destroy()
    }

    @Test fun directSaveAfterGostEditPersistsRealBeanInsteadOfBarePlugin() = runBlocking {
        DataStore.profileCacheStore.reset()
        val controller = launchEditor()
        selectPlugin(controller.get(), "gost-plugin")
        changeRaw(controller.get(), "mode=grpc;mux;future=false")
        changeText(controller.get(), "gostPluginHost", "cdn.invalid")

        controller.get().saveAndExit()

        val saved = SagerDatabase.proxyDao.getAll().single().requireBean() as ShadowsocksBean
        assertEquals("gost-plugin;mode=grpc;host=cdn.invalid;mux;future=false", saved.plugin)
    }

    private fun launchEditor(): org.robolectric.android.controller.ActivityController<ShadowsocksSettingsActivity> {
        val group = ProxyGroup(name = "test")
        group.id = SagerDatabase.groupDao.createGroup(group)
        DataStore.selectedGroup = group.id
        return Robolectric.buildActivity(ShadowsocksSettingsActivity::class.java).setup()
    }

    private fun fragment(activity: ShadowsocksSettingsActivity) =
        activity.supportFragmentManager.fragments.single() as PreferenceFragmentCompat

    private fun selectPlugin(activity: ShadowsocksSettingsActivity, name: String) {
        val preference = fragment(activity).findPreference<SimpleMenuPreference>("pluginName")!!
        preference.callChangeListener(name)
        preference.value = name
    }

    private fun changeRaw(activity: ShadowsocksSettingsActivity, raw: String) =
        changeText(activity, "pluginConfig", raw)

    private fun changeText(activity: ShadowsocksSettingsActivity, key: String, text: String) {
        val preference = fragment(activity).findPreference<EditTextPreference>(key)!!
        preference.callChangeListener(text)
        preference.text = text
    }

    private fun assertEditorState(activity: ShadowsocksSettingsActivity, plugin: String, raw: String) {
        val current = fragment(activity)
        assertEquals(plugin, current.findPreference<SimpleMenuPreference>("pluginName")!!.value)
        assertEquals(raw, current.findPreference<EditTextPreference>("pluginConfig")!!.text)
    }

    private fun serializeProduction(activity: ShadowsocksSettingsActivity): ShadowsocksBean {
        val bean = ShadowsocksBean()
        with(activity) { bean.serialize() }
        return bean
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
        fragment.preferenceManager.sharedPreferences!!.edit()
            .putString("gostPluginMode", "mws")
            .putString("gostPluginHost", "cdn.invalid")
            .putString("gostPluginPath", "/gost")
            .putBoolean("gostPluginTls", true)
            .putString("gostPluginMux", "3")
            .putString("pluginConfig", fragment.raw.text)
            .apply()

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
