package io.nekohasekai.sagernet.ui.profile

import android.os.Bundle
import androidx.preference.EditTextPreference
import androidx.preference.PreferenceFragmentCompat
import androidx.preference.SwitchPreference
import io.nekohasekai.sagernet.R
import io.nekohasekai.sagernet.database.DataStore
import io.nekohasekai.sagernet.database.preference.EditTextPreferenceModifiers
import io.nekohasekai.sagernet.fmt.shadowsocks.ShadowsocksBean
import moe.matsuri.nb4a.proxy.PreferenceBinding
import moe.matsuri.nb4a.proxy.PreferenceBindingManager
import moe.matsuri.nb4a.proxy.Type

import moe.matsuri.nb4a.ui.SimpleMenuPreference

class ShadowsocksSettingsActivity : ProfileSettingsActivity<ShadowsocksBean>() {

    override fun createEntity() = ShadowsocksBean()

    private val pbm = PreferenceBindingManager()
    private val name = pbm.add(PreferenceBinding(Type.Text, "name"))
    private val serverAddress = pbm.add(PreferenceBinding(Type.Text, "serverAddress"))
    private val serverPort = pbm.add(PreferenceBinding(Type.TextToInt, "serverPort"))
    private val password = pbm.add(PreferenceBinding(Type.Text, "password"))
    private val method = pbm.add(PreferenceBinding(Type.Text, "method"))
    private val pluginName =
        pbm.add(PreferenceBinding(Type.Text, "pluginName").apply { disable = true })
    private val pluginConfig =
        pbm.add(PreferenceBinding(Type.Text, "pluginConfig").apply { disable = true })
    private val gostPluginMode =
        pbm.add(PreferenceBinding(Type.Text, "gostPluginMode").apply { disable = true })
    private val gostPluginHost =
        pbm.add(PreferenceBinding(Type.Text, "gostPluginHost").apply { disable = true })
    private val gostPluginPath =
        pbm.add(PreferenceBinding(Type.Text, "gostPluginPath").apply { disable = true })
    private val gostPluginTls =
        pbm.add(PreferenceBinding(Type.Bool, "gostPluginTls").apply { disable = true })
    private val gostPluginMux =
        pbm.add(PreferenceBinding(Type.Text, "gostPluginMux").apply { disable = true })
    private val sUoT = pbm.add(PreferenceBinding(Type.Bool, "sUoT"))
    private val enableMux = pbm.add(PreferenceBinding(Type.Bool, "enableMux"))
    private val muxType = pbm.add(PreferenceBinding(Type.TextToInt, "muxType"))
    private val muxConcurrency = pbm.add(PreferenceBinding(Type.TextToInt, "muxConcurrency"))
    private val muxMode = pbm.add(PreferenceBinding(Type.TextToInt, "muxMode"))
    private val muxMaxConnections = pbm.add(PreferenceBinding(Type.TextToInt, "muxMaxConnections"))
    private val muxMinStreams = pbm.add(PreferenceBinding(Type.TextToInt, "muxMinStreams"))
    private val muxPadding = pbm.add(PreferenceBinding(Type.Bool, "muxPadding"))
    private val muxBrutal = pbm.add(PreferenceBinding(Type.Bool, "muxBrutal"))
    private val muxBrutalUpMbps = pbm.add(PreferenceBinding(Type.TextToInt, "muxBrutalUpMbps"))
    private val muxBrutalDownMbps = pbm.add(PreferenceBinding(Type.TextToInt, "muxBrutalDownMbps"))

    private var syncingGostPreferences = false
    private var gostRawOptions = ""
    private val pluginConfigs = mutableMapOf<String, String>()

    override fun ShadowsocksBean.init() {
        pbm.writeToCacheAll(this)

        val currentPluginName = plugin.substringBefore(";")
        val currentConfig = plugin.substringAfter(';', "")
        val state = GostPluginUiState.fromPlugin(plugin)
        pluginConfigs[currentPluginName] = currentConfig
        if (state.enabled) gostRawOptions = currentConfig
        DataStore.profileCacheStore.putString("pluginName", currentPluginName)
        DataStore.profileCacheStore.putString("pluginConfig", currentConfig)
        if (state.enabled) writeGostState(state)
    }

    override fun ShadowsocksBean.serialize() {
        pbm.fromCacheAll(this)

        val pn = pluginName.readStringFromCache()
        val pc = if (pn == "gost-plugin") gostRawOptions else pluginConfig.readStringFromCache()
        plugin = if (pn == "gost-plugin") {
            if (pc.isBlank()) pn else "$pn;$pc"
        } else if (pn.isNotBlank()) {
            if (pc.isBlank()) pn else "$pn;$pc"
        } else {
            ""
        }
    }

    private fun syncStructuredFromRaw(raw: String) {
        val state = GostPluginUiState.fromPlugin(if (raw.isBlank()) "gost-plugin" else "gost-plugin;$raw")
        writeGostState(state)
    }

    private fun writeGostState(state: GostPluginUiState) {
        syncingGostPreferences = true
        try {
            val modePreference = gostPluginMode.preference as SimpleMenuPreference
            if (state.modeSupported) {
                modePreference.entries = resources.getTextArray(R.array.gost_plugin_modes)
                modePreference.entryValues = resources.getTextArray(R.array.gost_plugin_mode_values)
            } else {
                modePreference.entries = arrayOf("Custom (${state.mode})", "WebSocket", "Multiplexed WebSocket")
                modePreference.entryValues = arrayOf(state.mode, "ws", "mws")
            }
            DataStore.profileCacheStore.putString("gostPluginMode", state.mode)
            DataStore.profileCacheStore.putString("gostPluginHost", state.host)
            DataStore.profileCacheStore.putString("gostPluginPath", state.path)
            DataStore.profileCacheStore.putBoolean("gostPluginTls", state.tls)
            DataStore.profileCacheStore.putString("gostPluginMux", state.mux.editorText())
        } finally {
            syncingGostPreferences = false
        }
    }

    private fun mergeStructuredIntoRaw(changedKey: String, changedValue: Any?) {
        val current = GostPluginUiState.fromPlugin(
            if (gostRawOptions.isBlank()) "gost-plugin" else "gost-plugin;$gostRawOptions",
        )
        val mode = if (changedKey == "gostPluginMode") changedValue.toString() else current.mode
        val merged = current.copy(
            mode = mode,
            modeSupported = if (changedKey == "gostPluginMode") mode == "ws" || mode == "mws" else current.modeSupported,
            host = if (changedKey == "gostPluginHost") changedValue.toString() else current.host,
            path = if (changedKey == "gostPluginPath") changedValue.toString() else current.path,
            tls = if (changedKey == "gostPluginTls") changedValue as Boolean else current.tls,
            mux = if (changedKey == "gostPluginMux") GostMuxValue.fromEditor(changedValue.toString())
            else current.mux,
        )
        gostRawOptions = merged.mergeStructured().canonical()
        pluginConfigs["gost-plugin"] = gostRawOptions
        syncingGostPreferences = true
        try {
            DataStore.profileCacheStore.putString("pluginConfig", gostRawOptions)
        } finally {
            syncingGostPreferences = false
        }
    }

    override fun PreferenceFragmentCompat.createPreferences(
        savedInstanceState: Bundle?,
        rootKey: String?,
    ) {
        addPreferencesFromResource(R.xml.shadowsocks_preferences)
        pbm.setPreferenceFragment(this)

        serverPort.preference.apply {
            this as EditTextPreference
            setOnBindEditTextListener(EditTextPreferenceModifiers.Port)
        }
        password.preference.apply {
            this as EditTextPreference
            summaryProvider = PasswordSummaryProvider
        }

        val preferenceFragment = this
        val selectedPlugin = pluginName.readStringFromCache()
        configureGostPluginPreferences(preferenceFragment, selectedPlugin)
        pluginName.preference.apply {
            this as SimpleMenuPreference
            setOnPreferenceChangeListener { _, newValue ->
                val oldPlugin = pluginName.readStringFromCache()
                pluginConfigs[oldPlugin] = pluginConfig.readStringFromCache()
                val nextPlugin = newValue.toString()
                configureGostPluginPreferences(preferenceFragment, nextPlugin)
                val nextConfig = if (nextPlugin == "gost-plugin") {
                    gostRawOptions
                } else {
                    pluginConfigs[nextPlugin].orEmpty()
                }
                DataStore.profileCacheStore.putString("pluginConfig", nextConfig)
                if (nextPlugin == "gost-plugin") syncStructuredFromRaw(nextConfig)
                true
            }
        }

        pluginConfig.preference.setOnPreferenceChangeListener { _, newValue ->
            val raw = newValue.toString()
            val selected = pluginName.readStringFromCache()
            pluginConfigs[selected] = raw
            if (selected == "gost-plugin" && !syncingGostPreferences) {
                gostRawOptions = raw
                syncStructuredFromRaw(raw)
            }
            true
        }

        fun bindStructured(key: String, preference: androidx.preference.Preference) {
            preference.setOnPreferenceChangeListener { _, newValue ->
                if (!syncingGostPreferences && pluginName.readStringFromCache() == "gost-plugin") {
                    mergeStructuredIntoRaw(key, newValue)
                }
                true
            }
        }
        bindStructured("gostPluginMode", gostPluginMode.preference)
        bindStructured("gostPluginHost", gostPluginHost.preference)
        bindStructured("gostPluginPath", gostPluginPath.preference)
        bindStructured("gostPluginTls", gostPluginTls.preference)
        bindStructured("gostPluginMux", gostPluginMux.preference)

        // Mux mode visibility control
        muxMode.preference.apply {
            updateMuxMode(muxMode.readIntFromCache())
            this as SimpleMenuPreference
            setOnPreferenceChangeListener { _, newValue ->
                updateMuxMode((newValue as String).toInt())
                true
            }
        }

        muxBrutal.preference.apply {
            updateMuxBrutal(muxBrutal.readBoolFromCache())
            this as SwitchPreference
            setOnPreferenceChangeListener { _, newValue ->
                updateMuxBrutal(newValue as Boolean)
                true
            }
        }
    }

    private fun updateMuxBrutal(enabled: Boolean) {
        muxBrutalUpMbps.preference.isVisible = enabled
        muxBrutalDownMbps.preference.isVisible = enabled
    }

    private fun updateMuxMode(mode: Int) {
        // mode 0: max_streams mode - show muxConcurrency, hide muxMaxConnections/muxMinStreams
        // mode 1: connections mode - hide muxConcurrency, show muxMaxConnections/muxMinStreams
        val isMaxStreamsMode = mode == 0
        muxConcurrency.preference.isVisible = isMaxStreamsMode
        muxMaxConnections.preference.isVisible = !isMaxStreamsMode
        muxMinStreams.preference.isVisible = !isMaxStreamsMode
    }

}
