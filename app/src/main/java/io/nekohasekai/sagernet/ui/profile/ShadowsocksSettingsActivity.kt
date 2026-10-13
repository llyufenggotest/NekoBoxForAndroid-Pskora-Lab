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

    private companion object {
        const val PLUGIN_NAME_KEY = "pluginName"
        const val PLUGIN_CONFIG_KEY = "pluginConfig"
        const val GOST_PLUGIN = "gost-plugin"
        const val GOST_RAW_OPTIONS_KEY = "shadowsocksGostRawOptions"
        const val PLUGIN_CONFIGS_KEY = "shadowsocksPluginConfigs"
    }

    private var syncingGostPreferences = false
    private var gostRawOptions = ""
    private val pluginConfigs = mutableMapOf<String, String>()

    private fun persistPluginState(pluginName: String, raw: String) {
        if (pluginName.isNotBlank()) pluginConfigs[pluginName] = raw
        if (pluginName == GOST_PLUGIN) {
            gostRawOptions = raw
            DataStore.profileCacheStore.putString(GOST_RAW_OPTIONS_KEY, raw)
        }
        val json = org.json.JSONObject()
        pluginConfigs.forEach { (name, config) -> json.put(name, config) }
        DataStore.profileCacheStore.putString(PLUGIN_CONFIGS_KEY, json.toString())
    }

    private fun cachePluginConfig(pluginName: String, raw: String) {
        DataStore.profileCacheStore.putString(PLUGIN_CONFIG_KEY, raw)
        persistPluginState(pluginName, raw)
        if (!syncingGostPreferences) {
            syncingGostPreferences = true
            try {
                (pluginConfig.preference as EditTextPreference).text = raw
            } finally {
                syncingGostPreferences = false
            }
        }
    }

    private fun restorePluginState() {
        pluginConfigs.clear()
        DataStore.profileCacheStore.getString(PLUGIN_CONFIGS_KEY)?.let { encoded ->
            runCatching {
                val json = org.json.JSONObject(encoded)
                json.keys().forEach { name -> pluginConfigs[name] = json.getString(name) }
            }
        }
        gostRawOptions = DataStore.profileCacheStore.getString(GOST_RAW_OPTIONS_KEY)
            ?: pluginConfigs[GOST_PLUGIN].orEmpty()
        if (gostRawOptions.isNotEmpty()) pluginConfigs[GOST_PLUGIN] = gostRawOptions
        val selected = DataStore.profileCacheStore.getString(PLUGIN_NAME_KEY).orEmpty()
        val current = pluginConfigs[selected]
        if (current != null) DataStore.profileCacheStore.putString(PLUGIN_CONFIG_KEY, current)
    }

    override fun ShadowsocksBean.init() {
        pbm.writeToCacheAll(this)

        val currentPluginName = plugin.substringBefore(";")
        val currentConfig = plugin.substringAfter(';', "")
        val state = GostPluginUiState.fromPlugin(plugin)
        DataStore.profileCacheStore.putString(PLUGIN_NAME_KEY, currentPluginName)
        DataStore.profileCacheStore.putString(PLUGIN_CONFIG_KEY, currentConfig)
        pluginConfigs.clear()
        gostRawOptions = ""
        persistPluginState(currentPluginName, currentConfig)
        if (state.enabled) persistGostState(state)
    }

    override fun ShadowsocksBean.serialize() {
        pbm.fromCacheAll(this)

        val pn = pluginName.readStringFromCache()
        val pc = if (pn == GOST_PLUGIN) {
            DataStore.profileCacheStore.getString(GOST_RAW_OPTIONS_KEY) ?: gostRawOptions
        } else {
            pluginConfigs[pn] ?: pluginConfig.readStringFromCache()
        }
        plugin = if (pn == GOST_PLUGIN) {
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

    private fun persistGostState(state: GostPluginUiState) {
        DataStore.profileCacheStore.putString("gostPluginMode", state.mode)
        DataStore.profileCacheStore.putString("gostPluginHost", state.host)
        DataStore.profileCacheStore.putString("gostPluginPath", state.path)
        DataStore.profileCacheStore.putBoolean("gostPluginTls", state.tls)
        DataStore.profileCacheStore.putString("gostPluginMux", state.mux.editorText())
    }

    private fun syncGostPreferences(state: GostPluginUiState) {
        val modePreference = gostPluginMode.preference as SimpleMenuPreference
        if (state.modeSupported) {
            modePreference.entries = resources.getTextArray(R.array.gost_plugin_modes)
            modePreference.entryValues = resources.getTextArray(R.array.gost_plugin_mode_values)
        } else {
            modePreference.entries = arrayOf("Custom (${state.mode})", "WebSocket", "Multiplexed WebSocket")
            modePreference.entryValues = arrayOf(state.mode, "ws", "mws")
        }
    }

    private fun writeGostState(state: GostPluginUiState) {
        if (syncingGostPreferences) return
        syncingGostPreferences = true
        try {
            persistGostState(state)
            syncGostPreferences(state)
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
        cachePluginConfig(GOST_PLUGIN, gostRawOptions)
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
        restorePluginState()
        val selectedPlugin = pluginName.readStringFromCache()
        configureGostPluginPreferences(preferenceFragment, selectedPlugin)
        if (selectedPlugin == GOST_PLUGIN) syncStructuredFromRaw(gostRawOptions)
        pluginName.preference.apply {
            this as SimpleMenuPreference
            setOnPreferenceChangeListener { _, newValue ->
                val oldPlugin = pluginName.readStringFromCache()
                persistPluginState(oldPlugin, pluginConfig.readStringFromCache())
                val nextPlugin = newValue.toString()
                configureGostPluginPreferences(preferenceFragment, nextPlugin)
                val nextConfig = if (nextPlugin == GOST_PLUGIN) {
                    DataStore.profileCacheStore.getString(GOST_RAW_OPTIONS_KEY) ?: gostRawOptions
                } else {
                    pluginConfigs[nextPlugin].orEmpty()
                }
                cachePluginConfig(nextPlugin, nextConfig)
                if (nextPlugin == GOST_PLUGIN) syncStructuredFromRaw(nextConfig)
                true
            }
        }

        pluginConfig.preference.setOnPreferenceChangeListener { _, newValue ->
            val raw = newValue.toString()
            val selected = pluginName.readStringFromCache()
            cachePluginConfig(selected, raw)
            if (selected == GOST_PLUGIN && !syncingGostPreferences) {
                syncStructuredFromRaw(raw)
            }
            true
        }

        fun bindStructured(key: String, preference: androidx.preference.Preference) {
            preference.setOnPreferenceChangeListener { _, newValue ->
                if (!syncingGostPreferences && pluginName.readStringFromCache() == GOST_PLUGIN) {
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
