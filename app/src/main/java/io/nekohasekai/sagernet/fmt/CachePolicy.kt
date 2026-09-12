package io.nekohasekai.sagernet.fmt

import com.google.gson.JsonObject
import com.google.gson.JsonParser
import moe.matsuri.nb4a.SingBoxOptions.CacheFile
import moe.matsuri.nb4a.SingBoxOptions.ClashAPIOptions
import moe.matsuri.nb4a.SingBoxOptions.ExperimentalOptions

/** Raw full configurations also need the same test-only override. */
internal fun disableTestCache(config: String): String {
    val root = JsonParser.parseString(config).asJsonObject
    val experimental = root.getAsJsonObject("experimental") ?: JsonObject().also { root.add("experimental", it) }
    val cache = experimental.getAsJsonObject("cache_file") ?: JsonObject().also { experimental.add("cache_file", it) }
    cache.addProperty("enabled", false)
    return root.toString()
}

/** Test boxes retain platform logging but must never open the VPN's shared cache. */
internal fun buildExperimentalOptions(forTest: Boolean, enableClashAPI: Boolean) =
    ExperimentalOptions().apply {
        cache_file = CacheFile().apply {
            enabled = !forTest
            if (!forTest) {
                path = "../cache/cache.db"
                store_fakeip = true
            }
        }
        if (!forTest && enableClashAPI) {
            clash_api = ClashAPIOptions().apply {
                external_controller = "127.0.0.1:9090"
                external_ui = "../files/yacd"
            }
        }
    }
