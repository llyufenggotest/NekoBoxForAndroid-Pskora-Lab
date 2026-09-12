package io.nekohasekai.sagernet.bg.proto

import io.nekohasekai.sagernet.BuildConfig
import io.nekohasekai.sagernet.bg.BaseService
import io.nekohasekai.sagernet.bg.ServiceNotification
import io.nekohasekai.sagernet.database.ProxyEntity
import io.nekohasekai.sagernet.ktx.Logs
import io.nekohasekai.sagernet.ktx.runOnDefaultDispatcher
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.runBlocking
import moe.matsuri.nb4a.utils.JavaUtil

class ProxyInstance(profile: ProxyEntity, var service: BaseService.Interface? = null) :
    BoxInstance(profile) {

    var notTmp = true

    var lastSelectorGroupId = -1L
    var displayProfileName = ServiceNotification.genTitle(profile)

    // for TrafficLooper
    var looper: TrafficLooper? = null
    private val trafficScope = CoroutineScope(SupervisorJob() + Dispatchers.Default)

    override fun buildConfig() {
        super.buildConfig()
        lastSelectorGroupId = super.config.selectorGroupId
        //
        if (notTmp) Logs.d(config.config)
        if (notTmp && BuildConfig.DEBUG) Logs.d(JavaUtil.gson.toJson(config.trafficMap))
    }

    // only use this in temporary instance
    fun buildConfigTmp() {
        notTmp = false
        buildConfig()
    }

    override suspend fun init() {
        super.init()
        pluginConfigs.forEach { (_, plugin) ->
            val (_, content) = plugin
            Logs.d(content)
        }
    }

    override suspend fun loadConfig() {
        super.loadConfig()
    }

    override fun launch() {
        // Publish only controller options from the exact config being launched across :bg/UI.
        // Do not infer the API port/secret from yacdURL or the proxy authentication password.
        if (notTmp) {
            val options = com.google.gson.JsonParser.parseString(config.config).asJsonObject
                .getAsJsonObject("experimental")?.get("clash_api")
            io.nekohasekai.sagernet.database.DataStore.configurationStore.putString(
                "activeClashApiOptions", options?.toString().orEmpty()
            )
        }
        box.setAsMain()
        super.launch() // start box
        // Publish the owner synchronously: close cannot miss a delayed global launch.
        looper = service?.let { TrafficLooper(it.data, trafficScope) }
        looper?.start()
    }

    override fun close() {
        try {
            runBlocking { looper?.stop() }
        } finally {
            looper = null
            trafficScope.cancel()
            super.close()
        }
    }
}
