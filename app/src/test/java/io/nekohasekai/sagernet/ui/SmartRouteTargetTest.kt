package io.nekohasekai.sagernet.ui

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class SmartRouteTargetTest {
    @Test fun separatesDomainsAndIpCidrsWithoutUserSyntax() {
        val parsed = parseSmartRouteTargets("example.com, 1.1.1.1\n10.0.0.0/8\n*.openai.com")
        assertEquals(listOf("domain:example.com", "domain:openai.com"), parsed.domains)
        assertEquals(listOf("1.1.1.1", "10.0.0.0/8"), parsed.ips)
        assertTrue(parsed.errors.isEmpty())
    }

    @Test fun acceptsExistingExplicitMatchersForCompatibility() {
        val parsed = parseSmartRouteTargets("geosite:openai\ndomain:claude.ai\ngeoip:cn")
        assertEquals(listOf("geosite:openai", "domain:claude.ai"), parsed.domains)
        assertEquals(listOf("geoip:cn"), parsed.ips)
    }

    @Test fun rejectsUrlsPathsAndInvalidTokens() {
        val parsed = parseSmartRouteTargets("https://example.com/path\nnot a target")
        assertEquals(2, parsed.errors.size)
    }

    @Test fun rejectsMalformedNumericAddresses() {
        for (token in listOf("999.1.1.1", "12345", "1.2.3", "1.1.1.1/33", "1.1.1.1/-1")) {
            assertEquals(token, listOf(token), parseSmartRouteTargets(token).errors)
        }
    }

    @Test fun preservesRegexCase() {
        assertEquals(listOf("regexp:^API[.]Example"), parseSmartRouteTargets("regexp:^API[.]Example").domains)
    }

    @Test fun acceptsIpv6Literals() {
        assertEquals(listOf("::1", "2001:db8::/32"), parseSmartRouteTargets("::1\n2001:db8::/32").ips)
    }

    @Test fun mixedTargetsCannotBecomeAnAndRule() {
        assertTrue(parseSmartRouteTargets("example.com\n1.1.1.1").simpleModeError() != null)
        assertEquals(null, parseSmartRouteTargets("example.com").simpleModeError())
        assertTrue(parseSmartRouteTargets("").simpleModeError() != null)
    }

    @Test fun includesEveryRequestedServiceWithoutImplicitRules() {
        assertEquals(setOf("youtube", "tiktok", "telegram", "netflix", "disney", "x", "meta", "spotify", "google", "ai"), smartRoutePresets().map { it.id }.toSet())
        smartRoutePresets().forEach { assertEquals(null, parseSmartRouteTargets(it.targets.joinToString("\n")).simpleModeError()) }
    }

    @Test fun aiPresetCoversRequestedProviders() {
        val preset = smartRoutePresets().single { it.id == "ai" }
        val text = preset.targets.joinToString(" ").lowercase()
        for (required in listOf("openai", "claude", "gemini", "grok")) {
            assertTrue(required, text.contains(required))
        }
    }
}
