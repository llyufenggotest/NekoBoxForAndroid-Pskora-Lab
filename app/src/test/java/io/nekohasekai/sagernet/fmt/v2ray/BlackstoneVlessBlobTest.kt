package io.nekohasekai.sagernet.fmt.v2ray

import com.esotericsoftware.kryo.io.ByteBufferInput
import com.esotericsoftware.kryo.io.ByteBufferOutput
import io.nekohasekai.sagernet.fmt.KryoConverters
import org.junit.Assert.*
import org.junit.Test

class BlackstoneVlessBlobTest {
    /** Independent v10 layout, not a current serializer with its version overwritten. */
    private fun oldBlob(type: String, alterId: Int): ByteArray {
        val out = ByteBufferOutput(1024, -1)
        out.writeInt(10)
        out.writeString("legacy.example"); out.writeInt(443)
        out.writeString("12345678-1234-1234-1234-123456789abc#juzi")
        out.writeString(""); out.writeString("none"); out.writeInt(alterId)
        out.writeString(type)
        when (type) {
            "ws" -> { out.writeString("cdn.example"); out.writeString("/legacy"); out.writeInt(32); out.writeString("Sec-WebSocket-Protocol") }
            "xhttp" -> { out.writeString("cdn.example"); out.writeString("/legacy"); out.writeString("auto"); out.writeString("{\"fixture\":true}") }
            "kcp" -> { out.writeString("seed"); out.writeString("srtp"); out.writeInt(1350); out.writeInt(40); out.writeInt(3) }
        }
        out.writeString("tls"); out.writeString("sni.example"); out.writeString("h2")
        out.writeString("fixture-cert"); out.writeBoolean(false); out.writeString("chrome")
        out.writeString("fixture-public"); out.writeString("abcd")
        out.writeBoolean(true); out.writeString("fixture-ech"); out.writeInt(2)
        out.writeBoolean(true); out.writeBoolean(true); out.writeInt(1); out.writeInt(8)
        out.writeInt(1); out.writeInt(4); out.writeInt(2)
        out.writeBoolean(true); out.writeInt(100); out.writeInt(200)
        out.writeInt(1); out.writeString("legacy name")
        out.writeString("{\"fixture\":1}"); out.writeString("{\"fixture\":2}")
        return out.toBytes()
    }

    @Test fun oldV10BlobsLoadWithoutPrivateOptionsAndUpgradeLosslessly() {
        for (type in listOf("tcp", "ws", "xhttp", "kcp")) for (alterId in listOf(-1, 0)) {
            val bean = KryoConverters.vmessDeserialize(oldBlob(type, alterId))
            assertNull(bean.blackstoneE); assertNull(bean.blackstoneH)
            assertEquals(type, bean.type); assertEquals(alterId, bean.alterId.toInt())
            assertEquals("legacy name", bean.name)
            assertEquals("{\"fixture\":1}", bean.customOutboundJson)
            assertEquals("{\"fixture\":2}", bean.customConfigJson)
            assertEquals("fixture-ech", bean.echConfig)
            assertEquals(200, bean.muxBrutalDownMbps.toInt())
            if (type == "kcp") assertEquals(3, bean.kcpCwndMultiplier.toInt())
            if (type == "xhttp") assertEquals("{\"fixture\":true}", bean.xhttpExtra)
            assertEquals(bean, bean.clone())
        }
    }

    @Test fun v11EncodesPresenceSeparatelyFromFalseAndRetainsAbstractExtras() {
        for (e in listOf(null, false, true)) for (h in listOf(null, "", "fixture-h")) {
            val bean = KryoConverters.vmessDeserialize(oldBlob("ws", -1)).apply {
                blackstoneE = e; blackstoneH = h
            }
            val bytes = KryoConverters.serialize(bean)
            assertEquals(11, ByteBufferInput(bytes).readInt())
            val restored = KryoConverters.vmessDeserialize(bytes)
            assertEquals(e, restored.blackstoneE); assertEquals(h, restored.blackstoneH)
            assertEquals("legacy name", restored.name)
            assertEquals("{\"fixture\":1}", restored.customOutboundJson)
            assertEquals("{\"fixture\":2}", restored.customConfigJson)
            assertEquals(bean, restored.clone())
        }
    }
}
