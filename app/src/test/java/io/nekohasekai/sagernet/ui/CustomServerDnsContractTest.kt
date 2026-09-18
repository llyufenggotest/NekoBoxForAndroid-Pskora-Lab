package io.nekohasekai.sagernet.ui

import java.io.File
import org.junit.Assert.assertTrue
import org.junit.Test

class CustomServerDnsContractTest {
    @Test
    fun acceptsUdpAndTcpPrivateDnsSchemes() {
        val files = listOf(
            File("src/main/java/io/nekohasekai/sagernet/ui/GroupSettingsActivity.kt"),
            File("app/src/main/java/io/nekohasekai/sagernet/ui/GroupSettingsActivity.kt")
        )
        val file = files.first { it.isFile }
        val source = file.readText()
        assertTrue(source.contains("\"udp\", \"tcp\""))
    }
}
