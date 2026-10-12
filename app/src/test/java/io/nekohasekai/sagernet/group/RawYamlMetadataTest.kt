package io.nekohasekai.sagernet.group

import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class RawYamlMetadataTest {
    private fun parse(yaml: String) = runBlocking { RawUpdater.parseRawWithMetadata(yaml, "nodes.yaml") }

    @Test fun proxyPolicyWinsAndExtractsAllResolversWithoutUsingGlobalDnsLists() {
        val result = parse(
            """
            dns:
              proxy-server-nameserver-policy:
                +.edge.example: [udp://10.0.0.53:53, tcp://10.0.0.54:5353, udp://10.0.0.53:53]
              nameserver-policy:
                +.edge.example: tls://fallback.example:853
              nameserver: [https://public.example/dns-query]
              default-nameserver: [1.1.1.1]
              proxy-server-nameserver: [8.8.8.8]
              direct-nameserver: [9.9.9.9]
            proxies:
              - {name: one, type: ss, server: api.edge.example, port: 443, cipher: aes-128-gcm, password: test-only}
            """.trimIndent(),
        )
        assertEquals("udp://10.0.0.53:53,tcp://10.0.0.54:5353", result.metadata.groupDns)
    }

    @Test fun hostsMapOnlyServerSupportsChainsAndPolicySeesBeforeAndAfterNames() {
        val result = parse(
            """
            hosts:
              API.EXAMPLE.COM.: edge.internal
              edge.internal: [not-an-ip, 192.0.2.44, 192.0.2.45]
            dns:
              nameserver-policy:
                "*.example.com": udp://192.0.2.53:53
                192.0.2.44: tcp://192.0.2.54:53
            proxies:
              - name: one
                type: trojan
                server: api.example.com
                port: 443
                password: test-only
                sni: secure.example.com
            """.trimIndent(),
        )
        val bean = result.proxies.single()
        assertEquals("192.0.2.44", bean.serverAddress)
        assertEquals("secure.example.com", (bean as io.nekohasekai.sagernet.fmt.trojan.TrojanBean).sni)
        assertEquals("udp://192.0.2.53:53,tcp://192.0.2.54:53", result.metadata.groupDns)
    }

    @Test fun matchedProxyPolicyWithLiteralUrlDoesNotFallThroughToNameserverPolicy() {
        val result = parse(
            """
            dns:
              proxy-server-nameserver-policy: {literal.test: "@url:https://never-fetch.invalid/list"}
              nameserver-policy: {literal.test: udp://10.0.0.9:53}
            proxies: [{name: one, type: ss, server: literal.test, port: 443, cipher: aes-128-gcm, password: test-only}]
            """.trimIndent(),
        )
        assertNull(result.metadata.groupDns)
    }

    @Test fun mappingCanMatchOnlyThePostHostsServer() {
        val result = parse(
            """
            hosts: {alias.example: mapped.internal}
            dns:
              nameserver-policy: {mapped.internal: tcp://192.0.2.60:53}
            proxies: [{name: one, type: ss, server: alias.example, port: 443, cipher: aes-128-gcm, password: test-only}]
            """.trimIndent(),
        )
        assertEquals("mapped.internal", result.proxies.single().serverAddress)
        assertEquals("tcp://192.0.2.60:53", result.metadata.groupDns)
    }

    @Test fun wildcardSemanticsCyclesInvalidAtomsLiteralUrlAndConflictsAreSafe() {
        val rootOnly = parse(
            """
            dns:
              nameserver-policy:
                "*.example.com": udp://10.0.0.1:53
                "+.root.test": udp://10.0.0.2:53
                literal.test: "@url:https://should-not-be-fetched.invalid/list"
            hosts:
              loop.test: loop2.test
              loop2.test: loop.test
              bad.test: "bad host value"
            proxies:
              - {name: root, type: ss, server: example.com, port: 443, cipher: aes-128-gcm, password: test-only}
              - {name: plus, type: ss, server: root.test, port: 443, cipher: aes-128-gcm, password: test-only}
              - {name: loop, type: ss, server: loop.test, port: 443, cipher: aes-128-gcm, password: test-only}
            """.trimIndent(),
        )
        assertEquals("example.com", rootOnly.proxies[0].serverAddress)
        assertEquals("loop.test", rootOnly.proxies[2].serverAddress)
        assertEquals("udp://10.0.0.2:53", rootOnly.metadata.groupDns)
        assertTrue(!rootOnly.metadata.hasPolicyConflict)
    }

    @Test fun zipEntryMetadataIsIsolatedAndConflictsOrExistingValuesDoNotAutofill() {
        val first = parse(
            """
            dns: {nameserver-policy: {one.test: udp://10.0.0.1:53}}
            proxies: [{name: one, type: ss, server: one.test, port: 443, cipher: aes-128-gcm, password: test-only}]
            """.trimIndent(),
        )
        val second = parse(
            """
            dns: {nameserver-policy: {two.test: tcp://10.0.0.2:53}}
            proxies: [{name: two, type: ss, server: two.test, port: 443, cipher: aes-128-gcm, password: test-only}]
            """.trimIndent(),
        )
        assertNull(RawUpdater.groupDnsForFileImport(listOf(first, second), ""))
        assertEquals("user.example:53", RawUpdater.groupDnsForFileImport(listOf(first), "user.example:53"))
        assertEquals("udp://10.0.0.1:53", RawUpdater.groupDnsForFileImport(listOf(first), ""))
    }

    @Test fun exactAndPlusAndStarDomainMatchingAreDistinct() {
        assertTrue(RawUpdater.policyDomainMatches("example.com", "EXAMPLE.COM."))
        assertTrue(RawUpdater.policyDomainMatches("+.example.com", "example.com"))
        assertTrue(RawUpdater.policyDomainMatches("+.example.com", "a.example.com"))
        assertTrue(RawUpdater.policyDomainMatches("*.example.com", "a.example.com"))
        assertTrue(!RawUpdater.policyDomainMatches("*.example.com", "example.com"))
        assertTrue(!RawUpdater.policyDomainMatches("example.com", "a.example.com"))
    }
}
