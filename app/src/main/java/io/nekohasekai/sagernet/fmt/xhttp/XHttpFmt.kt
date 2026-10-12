package io.nekohasekai.sagernet.fmt.xhttp

import moe.matsuri.nb4a.SingBoxOptions

// 拼装出送给 Go 引擎的最终配置 (对应 Go 的 XHttpOutboundOptions)
fun buildSingBoxOutboundXHttpBean(bean: XHttpBean): SingBoxOptions.Outbound_XHttpOptions {
    val hasDevicePair = bean.deviceId.isNotBlank() && bean.deviceName.isNotBlank()
    return SingBoxOptions.Outbound_XHttpOptions().apply {
        type = if (hasDevicePair) "Blackstone" else "xhttp"
        name = bean.name
        server = bean.serverAddress
        server_port = bean.serverPort
        password = bean.password
        node_id = bean.nodeId
        if (hasDevicePair) {
            device_id = bean.deviceId
            device_name = bean.deviceName
        }
    }
}
