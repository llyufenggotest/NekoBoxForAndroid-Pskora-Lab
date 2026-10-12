package io.nekohasekai.sagernet.fmt

import io.nekohasekai.sagernet.GroupType
import io.nekohasekai.sagernet.database.ProxyGroup
import io.nekohasekai.sagernet.database.SubscriptionBean
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class GroupDnsInputTest {
    private fun subscription(newValue: String?, legacy: String?) = ProxyGroup(
        type = GroupType.SUBSCRIPTION,
        customDirectDns = legacy,
        subscription = SubscriptionBean().apply {
            initializeDefaultValues()
            serverDnsResolver = newValue
        },
    )

    @Test fun subscriptionReadsNewFieldThenLegacyAndSaveDualWrites() {
        val group = subscription(" tcp://192.0.2.1:53 ", "udp://192.0.2.2:53")
        assertEquals("tcp://192.0.2.1:53", group.groupDnsInput())
        group.setGroupDnsInput(" udp://192.0.2.3:53, tcp://192.0.2.4:53 ")
        assertEquals("udp://192.0.2.3:53,tcp://192.0.2.4:53", group.customDirectDns)
        assertEquals(group.customDirectDns, group.subscription!!.serverDnsResolver)

        group.setGroupDnsInput("")
        assertEquals("", group.customDirectDns)
        assertEquals("", group.subscription!!.serverDnsResolver)

        val legacy = subscription("  ", " tls://dns.internal:853 ")
        assertEquals("tls://dns.internal:853", legacy.groupDnsInput())
        legacy.type = GroupType.BASIC
        assertEquals("tls://dns.internal:853", legacy.groupDnsInput())
    }

    @Test fun commaSeparatedResolversAreTrimmedDeduplicatedAndValidatedIndividually() {
        val raw = " udp://192.0.2.1:53,https://dns.example/dns-query?token=x,udp://192.0.2.1:53,tls://dns.example:853,quic://dns.example:853,host.internal:5353 "
        assertEquals(
            listOf(
                "udp://192.0.2.1:53",
                "https://dns.example/dns-query?token=x",
                "tls://dns.example:853",
                "quic://dns.example:853",
                "host.internal:5353",
            ),
            parseGroupDnsResolvers(raw),
        )
        assertEquals(parseGroupDnsResolvers(raw)!!.joinToString(","), normalizeGroupDnsInput(raw))
        assertTrue(isValidGroupDnsInput(raw))
        assertTrue(isValidGroupDnsInput(""))
        assertFalse(isValidGroupDnsInput("udp://192.0.2.1:53, ftp://bad.example"))
        assertFalse(isValidGroupDnsInput("udp://192.0.2.1:53,,tcp://192.0.2.2:53"))
        assertFalse(isValidGroupDnsInput("https://dns.example/dns-query,https://"))
        assertNull(normalizeGroupDnsInput("tcp://dns.example:bad"))
    }
}
