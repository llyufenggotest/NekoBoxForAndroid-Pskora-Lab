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
        assertTrue(source.contains("serverDnsResolverFor(bean, ownerGroup)"))
        assertTrue(source.contains("\"domain_resolver\""))
        assertTrue(!source.contains("if (!legacyDns.isNullOrBlank() && nodeDomainList.isNotEmpty())"))
        val policy = File(builder.parentFile, "GroupDnsPolicy.kt").readText()
        assertTrue(policy.contains("clean(group.subscription?.serverDnsResolver)"))
        assertTrue(policy.contains("clean(group?.customDirectDns)"))
    }
}
