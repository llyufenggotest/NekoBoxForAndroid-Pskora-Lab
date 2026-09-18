package io.nekohasekai.sagernet.ui

import java.io.File
import org.junit.Assert.assertTrue
import org.junit.Test

class CustomServerDnsContractTest {
    @Test
    fun acceptsUdpAndTcpPrivateDnsSchemes() {
        val file = File("app/src/main/java/io/nekohasekai/sagernet/ui/GroupSettingsActivity.kt")
        val source = file.readText()
        assertTrue(source.contains("\"udp\", \"tcp\""))
    }
}
