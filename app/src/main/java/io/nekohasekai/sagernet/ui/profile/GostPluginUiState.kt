package io.nekohasekai.sagernet.ui.profile

import androidx.preference.EditTextPreference
import androidx.preference.PreferenceFragmentCompat
import androidx.preference.SwitchPreference
import io.nekohasekai.sagernet.fmt.shadowsocks.GostPluginOptions
import moe.matsuri.nb4a.ui.SimpleMenuPreference

sealed class GostMuxValue {
    data object Missing : GostMuxValue()
    data object Flag : GostMuxValue()
    data class Text(val value: String) : GostMuxValue()

    fun editorText(): String = when (this) {
        Missing -> ""
        Flag -> FLAG_EDITOR_TEXT
        is Text -> value
    }

    companion object {
        const val FLAG_EDITOR_TEXT = "true"

        fun fromOptions(options: GostPluginOptions): GostMuxValue = when {
            !options.contains("mux") -> Missing
            options.flag("mux") -> Flag
            else -> Text(options.value("mux").orEmpty())
        }

        fun fromEditor(text: String): GostMuxValue = when {
            text.isBlank() -> Missing
            text.equals(FLAG_EDITOR_TEXT, ignoreCase = true) -> Flag
            else -> Text(text)
        }
    }
}

data class GostPluginUiState(
    val enabled: Boolean,
    val originalPlugin: String,
    val rawOptions: String = "",
    val mode: String = "ws",
    val modeSupported: Boolean = true,
    val host: String = "",
    val path: String = "",
    val tls: Boolean = false,
    val mux: GostMuxValue = GostMuxValue.Missing,
) {
    fun toPlugin(pluginName: String): String {
        if (!enabled || pluginName != "gost-plugin") return originalPlugin
        val canonical = mergeStructured().canonical()
        return if (canonical.isEmpty()) "gost-plugin" else "gost-plugin;$canonical"
    }

    fun mergeStructured(): GostPluginOptions {
        var options = GostPluginOptions.parse(rawOptions)
        if (modeSupported) options = options.withValue("mode", encodedMode())
        options = options.withValue("host", host.takeIf { it.isNotBlank() })
        options = options.withValue("path", path.takeIf { it.isNotBlank() })
        options = when (val value = mux) {
            GostMuxValue.Missing -> options.without("mux")
            GostMuxValue.Flag -> options.withFlag("mux")
            is GostMuxValue.Text -> options.withValue("mux", value.value)
        }
        return options
    }

    private fun encodedMode(): String {
        val base = if (mode == "mws") "mws" else "ws"
        return if (tls) if (base == "mws") "mwss" else "wss" else base
    }

    companion object {
        private val supportedModes = setOf("ws", "wss", "mws", "mwss")

        fun fromPlugin(plugin: String): GostPluginUiState {
            if (plugin.substringBefore(';') != "gost-plugin") {
                return GostPluginUiState(false, plugin)
            }
            val raw = plugin.substringAfter(';', "")
            val options = GostPluginOptions.parse(raw)
            val encodedMode = options.value("mode") ?: "ws"
            val supported = encodedMode in supportedModes
            return GostPluginUiState(
                enabled = true,
                originalPlugin = plugin,
                rawOptions = raw,
                mode = when (encodedMode) {
                    "ws", "wss" -> "ws"
                    "mws", "mwss" -> "mws"
                    else -> encodedMode
                },
                modeSupported = supported,
                host = options.value("host") ?: "",
                path = options.value("path") ?: "",
                tls = supported && encodedMode.endsWith("ss"),
                mux = GostMuxValue.fromOptions(options),
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
