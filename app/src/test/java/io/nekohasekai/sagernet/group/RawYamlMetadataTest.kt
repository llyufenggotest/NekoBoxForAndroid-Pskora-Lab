package io.nekohasekai.sagernet.group

import io.nekohasekai.sagernet.GroupType
import io.nekohasekai.sagernet.database.ProxyGroup
import io.nekohasekai.sagernet.database.SubscriptionBean
import io.nekohasekai.sagernet.fmt.setGroupDnsInput
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

    @Test fun leadingUnsupportedProxyDoesNotShiftHostsOrPolicyOntoSupportedProxy() {
        val result = parse(
            """
            hosts:
              unsupported.test: 192.0.2.10
              supported.test: 192.0.2.11
            dns:
              nameserver-policy:
                unsupported.test: udp://192.0.2.20:53
                supported.test: tcp://192.0.2.21:53
            proxies:
              - {name: ignored, type: unsupported, server: unsupported.test, port: 443}
              - {name: kept, type: ss, server: supported.test, port: 443, cipher: aes-128-gcm, password: test-only}
            """.trimIndent(),
        )
        assertEquals(listOf("kept"), result.proxies.map { it.name })
        assertEquals("192.0.2.11", result.proxies.single().serverAddress)
        assertEquals("tcp://192.0.2.21:53", result.metadata.groupDns)
    }

    @Test fun middleUnsupportedProxyDoesNotShiftLaterHostsOrPolicy() {
        val result = parse(
            """
            hosts:
              first.test: 192.0.2.31
              ignored.test: 192.0.2.32
              last.test: 192.0.2.33
            dns:
              nameserver-policy:
                first.test: udp://192.0.2.41:53
                ignored.test: tls://ignored-dns.test:853
                last.test: udp://192.0.2.41:53
            proxies:
              - {name: first, type: ss, server: first.test, port: 443, cipher: aes-128-gcm, password: test-only}
              - {name: ignored, type: unsupported, server: ignored.test, port: 443}
              - {name: last, type: trojan, server: last.test, port: 443, password: test-only}
            """.trimIndent(),
        )
        assertEquals(listOf("first", "last"), result.proxies.map { it.name })
        assertEquals(listOf("192.0.2.31", "192.0.2.33"), result.proxies.map { it.serverAddress })
        assertEquals("udp://192.0.2.41:53", result.metadata.groupDns)
        assertTrue(!result.metadata.hasPolicyConflict)
    }

    @Test fun failedSupportedProxyDoesNotShiftLaterHostsOrPolicy() {
        val result = parse(
            """
            hosts:
              broken.test: 192.0.2.51
              healthy.test: 192.0.2.52
            dns:
              nameserver-policy:
                broken.test: udp://192.0.2.61:53
                healthy.test: quic://dns.healthy.test:853
            proxies:
              - {name: broken, type: vmess, server: broken.test, uuid: synthetic-uuid}
              - {name: healthy, type: ss, server: healthy.test, port: 443, cipher: aes-128-gcm, password: test-only}
            """.trimIndent(),
        )
        assertEquals(listOf("healthy"), result.proxies.map { it.name })
        assertEquals("192.0.2.52", result.proxies.single().serverAddress)
        assertEquals("quic://dns.healthy.test:853", result.metadata.groupDns)
    }

    @Test fun differentWinningResolverSetsConflictButEqualSetsMerge() {
        val conflict = parse(
            """
            dns:
              nameserver-policy:
                one.test: udp://192.0.2.71:53
                two.test: tcp://192.0.2.72:53
            proxies:
              - {name: one, type: ss, server: one.test, port: 443, cipher: aes-128-gcm, password: test-only}
              - {name: two, type: ss, server: two.test, port: 443, cipher: aes-128-gcm, password: test-only}
            """.trimIndent(),
        )
        assertNull(conflict.metadata.groupDns)
        assertTrue(conflict.metadata.hasPolicyConflict)

        val equal = parse(
            """
            dns:
              nameserver-policy:
                one.test: [udp://192.0.2.73:53, tcp://192.0.2.74:53]
                two.test: [tcp://192.0.2.74:53, udp://192.0.2.73:53]
            proxies:
              - {name: one, type: ss, server: one.test, port: 443, cipher: aes-128-gcm, password: test-only}
              - {name: two, type: ss, server: two.test, port: 443, cipher: aes-128-gcm, password: test-only}
              - {name: unmatched, type: ss, server: unmatched.test, port: 443, cipher: aes-128-gcm, password: test-only}
            """.trimIndent(),
        )
        assertEquals("udp://192.0.2.73:53,tcp://192.0.2.74:53", equal.metadata.groupDns)
        assertTrue(!equal.metadata.hasPolicyConflict)
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

    @Test fun subscriptionAutofillOnlyWritesEmptyGroupsAndUsesDualWrite() {
        fun group(value: String = "") = ProxyGroup(
            type = GroupType.SUBSCRIPTION,
            customDirectDns = value,
            subscription = SubscriptionBean().apply {
                initializeDefaultValues()
                serverDnsResolver = value
            },
        )
        val metadata = RawUpdater.RawMetadata(groupDns = "udp://192.0.2.81:53")

        val empty = group()
        assertTrue(RawUpdater.applySubscriptionGroupDns(empty, metadata))
        assertEquals("udp://192.0.2.81:53", empty.customDirectDns)
        assertEquals(empty.customDirectDns, empty.subscription!!.serverDnsResolver)

        val existing = group("tcp://192.0.2.82:53")
        assertTrue(!RawUpdater.applySubscriptionGroupDns(existing, metadata))
        assertEquals("tcp://192.0.2.82:53", existing.customDirectDns)
        assertEquals(existing.customDirectDns, existing.subscription!!.serverDnsResolver)

        existing.setGroupDnsInput("")
        assertTrue(RawUpdater.applySubscriptionGroupDns(existing, metadata))
        assertEquals("udp://192.0.2.81:53", existing.customDirectDns)
        assertEquals(existing.customDirectDns, existing.subscription!!.serverDnsResolver)

        val conflict = group()
        assertTrue(!RawUpdater.applySubscriptionGroupDns(
            conflict,
            RawUpdater.RawMetadata(groupDns = "udp://192.0.2.83:53", hasPolicyConflict = true),
        ))
        assertEquals("", conflict.customDirectDns)
        assertEquals("", conflict.subscription!!.serverDnsResolver)
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
