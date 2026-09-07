package io.nekohasekai.sagernet.utils

import org.junit.Assert.*
import org.junit.Test

class ClashConnectionsTest {
    @Test fun mixedTaggedInboundIsSharedClient() {
        val result = ClashConnections.parse("""{"connections":[{"metadata":{"type":"mixed/mixed-in","sourceIP":"192.168.43.2"}}]}""", emptySet())
        assertEquals(mapOf("192.168.43.2" to 1), result.clients)
    }
    @Test fun unknownTypeIsDiagnosticNotEmpty() {
        val result = ClashConnections.parse("""{"connections":[{"metadata":{"type":"new/other","sourceIP":"192.168.43.2"}}]}""", emptySet())
        assertEquals(1, result.unclassified)
        assertTrue(result.describe().contains("未分类"))
    }
    @Test fun localMappedIpv4AndTunAreExcluded() {
        val result = ClashConnections.parse("""{"connections":[{"metadata":{"type":"mixed/in","sourceIP":"::ffff:192.168.1.1"}},{"metadata":{"type":"tun/tun-in","sourceIP":"192.168.1.2"}},{"metadata":{"inboundType":"socks","sourceIP":"192.168.1.3"}},{"metadata":{"type":"http","sourceIP":"192.168.1.3"}}]}""", setOf("192.168.1.1"))
        assertEquals(mapOf("192.168.1.3" to 2), result.clients)
        assertEquals(1, result.local)
        assertEquals(1, result.nonProxy)
    }
    @Test(expected = IllegalArgumentException::class) fun missingArrayIsNotEmpty() { ClashConnections.parse("{}", emptySet()) }
    @Test fun nullConnectionsMeansNoActiveConnections() { assertTrue(ClashConnections.parse("""{"connections":null}""", emptySet()).clients.isEmpty()) }
    @Test fun controllerUsesActualPortAndSecret() {
        val endpoint = ClashConnections.endpoint("""{"external_controller":"0.0.0.0:19090","secret":"test-secret"}""")
        assertEquals("http://127.0.0.1:19090/connections", endpoint.url)
        assertEquals("Bearer test-secret", endpoint.authorization)
    }
}
