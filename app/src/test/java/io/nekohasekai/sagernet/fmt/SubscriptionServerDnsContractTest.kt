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
        assertTrue(source.contains("perGroupResolver[ownerGid] = resolver"))
        assertTrue(source.contains("group?.customDirectDns?.takeIf { group.type != GroupType.SUBSCRIPTION }"))
        assertTrue(source.contains("if (!legacyDns.isNullOrBlank() && nodeDomainList.isNotEmpty())"))
    }
}
