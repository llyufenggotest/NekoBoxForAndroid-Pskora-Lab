package io.nekohasekai.sagernet.fmt

import java.io.File
import org.junit.Assert.assertTrue
import org.junit.Test

class SubscriptionServerDnsContractTest {
    @Test
    fun subscriptionDnsIsAuthoritativeAndLegacyGroupDnsIsNotEmitted() {
        val builder = File("src/main/java/io/nekohasekai/sagernet/fmt/ConfigBuilder.kt")
            .takeIf { it.isFile }
            ?: File("app/src/main/java/io/nekohasekai/sagernet/fmt/ConfigBuilder.kt")
        val source = builder.readText()
        assertTrue(source.contains("serverDnsResolversFor(bean, ownerGroup)"))
        assertTrue(source.contains("\"domain_resolver\""))
        assertTrue(source.contains("resolverTags.first()"))
        assertTrue(source.contains("core115 accepts one domain_resolver server"))
        assertTrue(!source.contains("if (!legacyDns.isNullOrBlank() && nodeDomainList.isNotEmpty())"))
        val policy = File(builder.parentFile, "GroupDnsPolicy.kt").readText()
        assertTrue(policy.contains("cleanGroupDns(subscription?.serverDnsResolver)"))
        assertTrue(policy.contains("cleanGroupDns(customDirectDns)"))
    }
}
