package io.nekohasekai.sagernet.ui

import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(manifest = Config.NONE, sdk = [28])
class PreferredRuntimeSamplesIntegrationTest {
    @Test fun binderJsonRoundTripMapsOnlyExactSourceIds() {
        val raw = JSONObject("""{"groupTag":"preferred","session":"service-session","samples":[{"tag":"source-a","delay":37,"sampleTime":1234,"source":"auto_urltest"},{"tag":"deleted","delay":1,"sampleTime":1234,"source":"auto_urltest"},{"tag":"source-b","sampleTime":1234,"source":"auto_urltest"}]}""")
        mapPreferredRuntimeSamples(raw, mapOf("source-a" to 91L, "source-b" to 92L))
        // Actual service/UI transport is a String; parse the returned bytes afresh.
        val received = JSONObject(raw.toString())
        assertEquals("service-session", received.getString("session"))
        val samples = readPreferredAutomatic(received)
        assertEquals(setOf(91L), samples.keys)
        assertEquals(37, samples.getValue(91L).ping)
        assertFalse(received.getJSONArray("samples").getJSONObject(0).has("tag"))
    }
}
