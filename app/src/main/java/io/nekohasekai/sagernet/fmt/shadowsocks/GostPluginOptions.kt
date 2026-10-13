package io.nekohasekai.sagernet.fmt.shadowsocks

/** Ordered SIP003 options with strict escaping for '\\', ';' and '='. */
class GostPluginOptions private constructor(
    private val entries: LinkedHashMap<String, String?>,
) {
    fun value(key: String): String? = entries[key]
    fun flag(key: String): Boolean = entries.containsKey(key) && entries[key] == null
    fun contains(key: String): Boolean = entries.containsKey(key)
    fun keys(): Set<String> = entries.keys

    fun with(key: String, value: String?): GostPluginOptions = fromEntries(entries.toMutableMap().apply {
        remove(key)
        if (value != null) put(key, value)
    })

    fun canonical(): String = entries.entries.joinToString(";") { (key, value) ->
        if (value == null) escape(key) else "${escape(key)}=${escape(value)}"
    }

    companion object {
        private val knownOrder = listOf(
            "mode", "host", "path", "serverName", "insecure", "nocomp", "ed", "mux",
        )

        fun empty() = GostPluginOptions(linkedMapOf())

        fun parse(raw: String): GostPluginOptions {
            if (raw.isBlank()) return empty()
            val result = linkedMapOf<String, String?>()
            splitEscaped(raw, ';').forEach { token ->
                require(token.isNotEmpty()) { "Empty gost-plugin option" }
                val separator = firstUnescaped(token, '=')
                val key = unescape(if (separator < 0) token else token.substring(0, separator))
                require(key.isNotEmpty()) { "Empty gost-plugin option key" }
                require(!result.containsKey(key)) { "Duplicate gost-plugin option: $key" }
                result[key] = if (separator < 0) null else unescape(token.substring(separator + 1))
            }
            return GostPluginOptions(order(result))
        }

        fun fromYaml(value: Any?): GostPluginOptions = when (value) {
            null -> empty()
            is String -> parse(value)
            is Map<*, *> -> {
                val result = linkedMapOf<String, String?>()
                value.forEach { (rawKey, rawValue) ->
                    val key = rawKey?.toString()?.takeIf { it.isNotEmpty() }
                        ?: throw IllegalArgumentException("Empty gost-plugin option key")
                    require(!result.containsKey(key)) { "Duplicate gost-plugin option: $key" }
                    when (rawValue) {
                        null -> result[key] = null
                        is Boolean -> if (key == "tls") {
                            result[key] = rawValue.toString()
                        } else if (rawValue) {
                            result[key] = null
                        }
                        else -> result[key] = rawValue.toString()
                    }
                }
                normalizeYaml(result)
            }
            else -> throw IllegalArgumentException("gost-plugin plugin-opts must be a map or string")
        }

        private fun normalizeYaml(source: LinkedHashMap<String, String?>): GostPluginOptions {
            val result = LinkedHashMap(source)
            val rawMode = result["mode"]?.lowercase()
            if (rawMode != null) result["mode"] = when (rawMode) {
                "websocket" -> if (
                    result.containsKey("mux") && result["mux"] != "false" && result["mux"] != "0"
                ) "mws" else "ws"
                else -> rawMode
            }
            val tls = result.remove("tls")
            if (tls.equals("true", ignoreCase = true)) {
                result["mode"] = when (result["mode"] ?: "ws") {
                    "ws" -> "wss"
                    "mws" -> "mwss"
                    "mwss", "wss" -> result["mode"]
                    else -> result["mode"]
                }
            }
            return GostPluginOptions(order(result))
        }

        private fun fromEntries(source: Map<String, String?>) = GostPluginOptions(order(source))

        private fun order(source: Map<String, String?>): LinkedHashMap<String, String?> {
            val result = linkedMapOf<String, String?>()
            knownOrder.forEach { key -> if (source.containsKey(key)) result[key] = source[key] }
            source.forEach { (key, value) -> if (!result.containsKey(key)) result[key] = value }
            return result
        }

        private fun splitEscaped(raw: String, delimiter: Char): List<String> {
            val result = mutableListOf<String>()
            val token = StringBuilder()
            var escaped = false
            raw.forEach { char ->
                when {
                    escaped -> {
                        token.append('\\').append(char)
                        escaped = false
                    }
                    char == '\\' -> escaped = true
                    char == delimiter -> {
                        result += token.toString()
                        token.setLength(0)
                    }
                    else -> token.append(char)
                }
            }
            require(!escaped) { "Trailing escape in gost-plugin options" }
            result += token.toString()
            return result
        }

        private fun firstUnescaped(raw: String, delimiter: Char): Int {
            var escaped = false
            raw.forEachIndexed { index, char ->
                when {
                    escaped -> escaped = false
                    char == '\\' -> escaped = true
                    char == delimiter -> return index
                }
            }
            require(!escaped) { "Trailing escape in gost-plugin option" }
            return -1
        }

        private fun unescape(raw: String): String {
            val result = StringBuilder()
            var escaped = false
            raw.forEach { char ->
                if (escaped) {
                    require(char == '\\' || char == ';' || char == '=') {
                        "Unsupported gost-plugin escape: \\$char"
                    }
                    result.append(char)
                    escaped = false
                } else if (char == '\\') {
                    escaped = true
                } else {
                    result.append(char)
                }
            }
            require(!escaped) { "Trailing escape in gost-plugin option" }
            return result.toString()
        }

        private fun escape(raw: String): String = buildString {
            raw.forEach { char ->
                if (char == '\\' || char == ';' || char == '=') append('\\')
                append(char)
            }
        }
    }
}

fun buildGostPlugin(value: Any?): String {
    val opts = GostPluginOptions.fromYaml(value).canonical()
    return if (opts.isEmpty()) "gost-plugin" else "gost-plugin;$opts"
}

fun ShadowsocksBean.gostPluginOptionsOrNull(): GostPluginOptions? {
    if (plugin.substringBefore(';') != "gost-plugin") return null
    return GostPluginOptions.parse(plugin.substringAfter(';', ""))
}
