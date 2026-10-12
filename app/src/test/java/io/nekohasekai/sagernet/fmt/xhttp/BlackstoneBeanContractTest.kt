package io.nekohasekai.sagernet.fmt.xhttp

import io.nekohasekai.sagernet.fmt.KryoConverters
import io.nekohasekai.sagernet.ktx.byteBuffer
import org.junit.Assert.assertEquals
import org.junit.Test
import java.io.ByteArrayOutputStream

class BlackstoneBeanContractTest {
    private fun bean() = XHttpBean().apply {
        serverAddress = "node.example"
        serverPort = 443
        name = "fixture"
        password = "token-fixture"
        nodeId = "node-fixture"
        deviceId = "device-fixture"
        deviceName = "tablet-fixture"
        initializeDefaultValues()
    }

    @Test fun kryoV2RoundTripPreservesDevices() {
        val restored = KryoConverters.xhttpDeserialize(KryoConverters.serialize(bean()))
        assertEquals("device-fixture", restored.deviceId)
        assertEquals("tablet-fixture", restored.deviceName)
    }

    @Test fun kryoV1LoadsWithEmptyDevices() {
        val source = bean()
        val bytes = ByteArrayOutputStream().also { stream ->
            val output = stream.byteBuffer()
            output.writeInt(1)
            output.writeString(source.serverAddress)
            output.writeInt(source.serverPort)
            output.writeString(source.password)
            output.writeString(source.nodeId)
            output.writeInt(1)
            output.writeString(source.name)
            output.writeString(source.customOutboundJson)
            output.writeString(source.customConfigJson)
            output.flush()
            output.close()
        }.toByteArray()
        val restored = KryoConverters.xhttpDeserialize(bytes)
        assertEquals("token-fixture", restored.password)
        assertEquals("node-fixture", restored.nodeId)
        assertEquals("", restored.deviceId)
        assertEquals("", restored.deviceName)
    }

    @Test fun builderUsesCanonicalTypeOnlyForCompleteDevicePair() {
        val canonical = buildSingBoxOutboundXHttpBean(bean())
        assertEquals("Blackstone", canonical.type)
        assertEquals("node-fixture", canonical.node_id)
        assertEquals("device-fixture", canonical.device_id)
        assertEquals("tablet-fixture", canonical.device_name)

        val legacy = bean().apply { deviceId = ""; deviceName = "" }
        val legacyOutbound = buildSingBoxOutboundXHttpBean(legacy)
        assertEquals("xhttp", legacyOutbound.type)
        assertEquals(null, legacyOutbound.device_id)
        assertEquals(null, legacyOutbound.device_name)
    }

}
