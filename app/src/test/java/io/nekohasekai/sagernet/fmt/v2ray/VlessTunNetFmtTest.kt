package io.nekohasekai.sagernet.fmt.v2ray

import java.util.Base64
import io.nekohasekai.sagernet.fmt.KryoConverters
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import moe.matsuri.nb4a.SingBoxOptions.Outbound_VLESSOptions
import okhttp3.HttpUrl.Companion.toHttpUrl

class VlessTunNetFmtTest {

    companion object {
        private const val FIXTURE_UUID = "123e4567-e89b-82d3-a456-426614174000"
    }

    private fun vless(id: String, name: String = "Fixture") = VMessBean().apply {
        alterId = -1
        uuid = id
        this.name = name
        initializeDefaultValues()
    }

    @Test
    fun suffixIsCaseInsensitiveAndRemovedOnlyForRuntime() {
        val bean = vless("$FIXTURE_UUID#TuNnEt")
        assertTrue(bean.isTunNet())
        assertEquals(FIXTURE_UUID, bean.tunNetRuntimeUuid())
        assertEquals("$FIXTURE_UUID#TuNnEt", bean.uuid)
    }

    @Test
    fun ordinaryVlessIsUntouched() {
        val bean = vless(FIXTURE_UUID)
        assertFalse(bean.isTunNet())
        assertEquals(FIXTURE_UUID, bean.tunNetRuntimeUuid())
        assertEquals(FIXTURE_UUID, bean.uuid)
    }

    @Test
    fun markerInDisplayNameDoesNotTrigger() {
        val bean = vless(FIXTURE_UUID, "Name #TunNet")
        assertFalse(bean.isTunNet())
        assertEquals(FIXTURE_UUID, bean.tunNetRuntimeUuid())
    }

    @Test
    fun similarSuffixDoesNotTrigger() {
        val id = "$FIXTURE_UUID#TunNet-extra"
        val bean = vless(id)
        assertFalse(bean.isTunNet())
        assertEquals(id, bean.tunNetRuntimeUuid())
    }

    @Test
    fun runtimeOutboundInjectsTunNetSnapshot() {
        val outbound = buildSingBoxOutboundStandardV2RayBean(
            vless("$FIXTURE_UUID#TunNet"),
            tunNetSnapshotPathProvider = { "/test/no-backup/tunnet/snapshot.json" },
        ) as Outbound_VLESSOptions
        assertEquals(FIXTURE_UUID, outbound.uuid)
        assertNotNull(outbound.tunnet)
        assertTrue(outbound.tunnet.snapshot.replace('\\', '/').endsWith("/tunnet/snapshot.json"))
        assertEquals(true, outbound.tunnet.front_proxy_strict)
    }

    @Test
    fun ordinaryVlessOutboundDoesNotInjectTunNet() {
        val outbound = buildSingBoxOutboundStandardV2RayBean(vless(FIXTURE_UUID)) as Outbound_VLESSOptions
        assertEquals(FIXTURE_UUID, outbound.uuid)
        assertNull(outbound.tunnet)
    }

    @Test
    fun tunNetMarkerSurvivesDatabaseSerialization() {
        val bean = vless("$FIXTURE_UUID#TuNnEt")
        val restored = KryoConverters.vmessDeserialize(KryoConverters.serialize(bean))

        assertEquals(bean.uuid, restored.uuid)
        assertTrue(restored.isTunNet())
        assertEquals(FIXTURE_UUID, restored.tunNetRuntimeUuid())
    }

    @Test
    fun tunNetVlessShareLinkRoundTripsMarker() {
        val bean = vless("$FIXTURE_UUID#TuNnEt", "").apply {
            serverAddress = "node.example"
            serverPort = 443
            type = "tcp"
            security = "none"
        }
        val link = bean.toUriVMessVLESSTrojan(false)
        val restored = VMessBean().apply {
            alterId = -1
            parseDuckSoft(link.replace("vless://", "https://").toHttpUrl())
        }

        assertEquals(bean.uuid, restored.uuid)
        assertTrue(restored.isTunNet())
        assertEquals(FIXTURE_UUID, restored.tunNetRuntimeUuid())
    }

    @Test
    fun ordinaryVlessShareLinkDoesNotGainMarker() {
        val bean = vless(FIXTURE_UUID, "").apply {
            serverAddress = "node.example"
            serverPort = 443
            type = "tcp"
            security = "none"
        }
        val link = bean.toUriVMessVLESSTrojan(false)
        val restored = VMessBean().apply {
            alterId = -1
            parseDuckSoft(link.replace("vless://", "https://").toHttpUrl())
        }

        assertEquals(FIXTURE_UUID, restored.uuid)
        assertFalse(restored.isTunNet())
        assertEquals(FIXTURE_UUID, restored.tunNetRuntimeUuid())
    }

    @Test
    fun selectorMarkerRoundTripsEntryAndHost() {
        val entry = "电信接入点"
        val encodedEntry = Base64.getUrlEncoder().withoutPadding().encodeToString(entry.toByteArray())
        val id = "$FIXTURE_UUID#TunNet:$encodedEntry:jp-01"
        val bean = vless(id, "").apply {
            serverAddress = "seed.example"
            serverPort = 443
            type = "tcp"
            security = "none"
        }
        val selection = bean.tunNetSelection()
        assertNotNull(selection)
        assertEquals(entry, selection!!.entryNode)
        assertEquals("jp-01", selection.hostSlug)
        assertEquals(FIXTURE_UUID, bean.tunNetRuntimeUuid())
        val link = bean.toUriVMessVLESSTrojan(false)
        val restored = VMessBean().apply {
            alterId = -1
            parseDuckSoft(link.replace("vless://", "https://").toHttpUrl())
        }
        assertEquals(id, restored.uuid)
        assertEquals(selection, restored.tunNetSelection())
    }

    @Test
    fun malformedSelectorDoesNotTriggerTunNet() {
        val bean = vless("$FIXTURE_UUID#TunNet:%:jp-01")
        assertFalse(bean.isTunNet())
        assertNull(bean.tunNetSelection())
    }

    @Test
    fun vmessNeverTriggersEvenWithSuffix() {
        val bean = VMessBean().apply {
            alterId = 0
            uuid = "$FIXTURE_UUID#TunNet"
            initializeDefaultValues()
        }
        assertFalse(bean.isTunNet())
        assertEquals("$FIXTURE_UUID#TunNet", bean.tunNetRuntimeUuid())
    }
}
