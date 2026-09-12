package moe.matsuri.nb4a.proxy.config

import com.esotericsoftware.kryo.io.ByteBufferInput
import com.esotericsoftware.kryo.io.ByteBufferOutput
import io.nekohasekai.sagernet.database.ProxyGroup
import io.nekohasekai.sagernet.fmt.KryoConverters
import org.junit.Assert.*
import org.junit.Test

class PreferredSerializationTest {
    private fun legacy(version: Int, type: Int = 0): ByteArray {
        val out = ByteBufferOutput(256, -1)
        out.writeInt(version)
        out.writeString("legacy.example")
        out.writeInt(443)
        out.writeInt(type)
        out.writeString("{\"type\":\"direct\"}")
        if (version >= 1) {
            out.writeInt(2); out.writeLong(41); out.writeLong(42)
            out.writeInt(60); out.writeInt(50)
        }
        if (version >= 2) { out.writeInt(1); out.writeLong(7) }
        out.writeInt(1)
        out.writeString("旧配置")
        out.writeString("")
        out.writeString("")
        return out.toBytes()
    }

    @Test fun versionZeroConfigAndOutboundRemainReadable() {
        for (type in listOf(0, 1)) {
            val bean = KryoConverters.configDeserialize(legacy(0, type))
            assertEquals(type, bean.type.toInt())
            assertEquals("旧配置", bean.name)
            assertEquals("legacy.example", bean.serverAddress)
            assertEquals("{\"type\":\"direct\"}", bean.config)
            assertEquals(emptyList<Long>(), bean.preferredMemberIds)
            assertEquals(emptyList<Long>(), bean.preferredSourceGroupIds)
            assertEquals(300, bean.preferredIntervalSeconds.toInt())
            assertEquals(0, bean.preferredMinDelayMilliseconds.toInt())
        }
    }

    @Test fun versionOnePreservesMembersAndDefaultsDynamicSources() {
        val bean = KryoConverters.configDeserialize(legacy(1, 2))
        assertEquals(listOf(41L, 42L), bean.preferredMemberIds)
        assertTrue(bean.preferredSourceGroupIds.isEmpty())
        assertEquals(60, bean.preferredIntervalSeconds.toInt())
        assertEquals(50, bean.preferredMinDelayMilliseconds.toInt())
        assertEquals("旧配置", bean.name)
    }

    @Test fun versionTwoPreservesDynamicSources() {
        val bean = KryoConverters.configDeserialize(legacy(2, 2))
        assertEquals(listOf(41L, 42L), bean.preferredMemberIds)
        assertEquals(listOf(7L), bean.preferredSourceGroupIds)
        assertEquals(60, bean.preferredIntervalSeconds.toInt())
        assertEquals(50, bean.preferredMinDelayMilliseconds.toInt())
        assertEquals("旧配置", bean.name)
    }

    @Test fun legacyVersionsDefaultToLatencyAndCurrentStableRoundTrips() {
        for (version in 0..2) {
            assertEquals(PREFERRED_MODE_LATENCY, KryoConverters.configDeserialize(legacy(version, 2)).preferredMode)
        }
        for (mode in listOf(PREFERRED_MODE_LATENCY, PREFERRED_MODE_STABLE)) {
            val bean = ConfigBean().apply {
                initializeDefaultValues(); type = 2; preferredMode = mode
                preferredMemberIds = listOf(41L)
            }
            val restored = KryoConverters.configDeserialize(KryoConverters.serialize(bean))
            assertEquals(mode, restored.preferredMode)
            assertEquals(mode, restored.preferredSpec().mode)
            assertEquals(mode, bean.clone().preferredMode)
        }
    }

    @Test fun currentDatabaseRoundTripAndClonePreserveIndependentLists() {
        val original = ConfigBean().apply {
            initializeDefaultValues(); type = 2; name = "自动优选"
            preferredMemberIds = arrayListOf(41L, Long.MAX_VALUE)
            preferredSourceGroupIds = arrayListOf(7L, 8L)
            preferredIntervalSeconds = 86400; preferredMinDelayMilliseconds = 65535
        }
        val bytes = KryoConverters.serialize(original)
        assertTrue(ByteBufferInput(bytes).readInt() >= 2)
        val restored = KryoConverters.configDeserialize(bytes)
        assertEquals(original.preferredSpec(), restored.preferredSpec())
        assertEquals(original.name, restored.name)
        assertEquals(2, restored.type.toInt())
        val clone = original.clone()
        clone.preferredMemberIds.clear(); clone.preferredSourceGroupIds.add(9L)
        assertEquals(listOf(41L, Long.MAX_VALUE), original.preferredMemberIds)
        assertEquals(listOf(7L, 8L), original.preferredSourceGroupIds)
    }

    @Test fun legacyGroupVersionsRemainReadableWithoutDetours() {
        for (version in listOf(0, 1)) {
            val out = ByteBufferOutput(256, -1)
            out.writeInt(version); out.writeLong(7); out.writeLong(3)
            out.writeBoolean(false); out.writeString("legacy"); out.writeInt(0); out.writeInt(0)
            if (version == 1) out.writeString("1.1.1.1")
            val restored = KryoConverters.deserialize(ProxyGroup(), out.toBytes())
            assertEquals(7L, restored.id)
            assertEquals("legacy", restored.name)
            assertEquals(if (version == 1) "1.1.1.1" else null, restored.customDirectDns)
            assertFalse(restored.isSelector)
            assertEquals(-1L, restored.frontProxy)
            assertEquals(-1L, restored.landingProxy)
        }
    }

    @Test fun groupBackupPreservesDetourReferencesAndSelector() {
        val original = ProxyGroup(id = 7, name = "来源", isSelector = true,
            frontProxy = 41, landingProxy = 42, customDirectDns = "https://dns.example/dns-query")
        val restored = KryoConverters.deserialize(ProxyGroup(), KryoConverters.serialize(original))
        assertEquals(original, restored)
    }
}
