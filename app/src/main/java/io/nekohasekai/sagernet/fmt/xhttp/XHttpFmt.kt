package io.nekohasekai.sagernet.fmt.xhttp

import moe.matsuri.nb4a.SingBoxOptions

// 拼装出送给 Go 引擎的最终配置 (对应 Go 的 XHttpOutboundOptions)
fun buildSingBoxOutboundXHttpBean(bean: XHttpBean): SingBoxOptions.Outbound_XHttpOptions {
    return SingBoxOptions.Outbound_XHttpOptions().apply {
        type = "xhttp"
        name = bean.name           // 🚀 核心：将节点名称传递给 Go 以供匹配
        server = bean.serverAddress
        server_port = bean.serverPort
        password = bean.password   // 传递 JWT Token
        node_id = bean.nodeId      // 传递节点 ID
    }
}