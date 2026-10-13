package io.nekohasekai.sagernet.ui.profile

import androidx.preference.EditTextPreference
import androidx.preference.PreferenceFragmentCompat
import androidx.preference.SwitchPreference
import io.nekohasekai.sagernet.fmt.shadowsocks.GostPluginOptions
import moe.matsuri.nb4a.ui.SimpleMenuPreference

data class GostPluginUiState(
    val enabled: Boolean,
    val originalPlugin: String,
    val rawOptions: String = "",
    val mode: String = "ws",
    val host: String = "",
    val path: String = "",
    val tls: Boolean = false,
    val mux: String = "",
) {
    fun toPlugin(pluginName: String): String {
        if (!enabled || pluginName != "gost-plugin") return originalPlugin
        var options = GostPluginOptions.parse(rawOptions)
        options = options.with("mode", encodedMode())
        options = options.with("host", host.takeIf { it.isNotBlank() })
        options = options.with("path", path.takeIf { it.isNotBlank() })
        options = options.with("mux", mux.takeIf { it.isNotBlank() })
        val canonical = options.canonical()
        return if (canonical.isEmpty()) "gost-plugin" else "gost-plugin;$canonical"
    }

    private fun encodedMode(): String {
        val base = when (mode) {
            "mws" -> "mws"
            else -> "ws"
        }
        return if (tls) when (base) {
            "mws" -> "mwss"
            else -> "wss"
        } else base
    }

    companion object {
        fun fromPlugin(plugin: String): GostPluginUiState {
            if (plugin.substringBefore(';') != "gost-plugin") {
                return GostPluginUiState(false, plugin)
            }
            val raw = plugin.substringAfter(';', "")
            val options = GostPluginOptions.parse(raw)
            val encodedMode = options.value("mode") ?: "ws"
            return GostPluginUiState(
                enabled = true,
                originalPlugin = plugin,
                rawOptions = raw,
                mode = if (encodedMode.startsWith("mw")) "mws" else "ws",
                host = options.value("host") ?: "",
                path = options.value("path") ?: "",
                tls = encodedMode.endsWith("ss") || encodedMode == "wss",
                mux = options.value("mux") ?: "",
            )
        }
    }
}

fun configureGostPluginPreferences(fragment: PreferenceFragmentCompat, pluginName: String) {
    val visible = pluginName == "gost-plugin"
    fragment.findPreference<SimpleMenuPreference>("gostPluginMode")?.isVisible = visible
    fragment.findPreference<EditTextPreference>("gostPluginHost")?.isVisible = visible
    fragment.findPreference<EditTextPreference>("gostPluginPath")?.isVisible = visible
    fragment.findPreference<SwitchPreference>("gostPluginTls")?.isVisible = visible
    fragment.findPreference<EditTextPreference>("gostPluginMux")?.isVisible = visible
    // The raw options editor stays visible for every plugin and remains the lossless source.
    fragment.findPreference<EditTextPreference>("pluginConfig")?.isVisible = true
}
