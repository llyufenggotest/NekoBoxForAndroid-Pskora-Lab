package io.nekohasekai.sagernet.group


import org.junit.Assert.assertEquals
import org.junit.Test

class FastupSingBoxImportTest {

    @Test
    fun singBoxTrojanWithMpwBecomesEditableFastupBean() {
        val outbound = RawUpdater.SingBoxTrojanFields(
            tag = "Fastup fixture",
            server = "node.example",
            serverPort = 443,
            password = "synthetic-password",
            mpw = "rotated-mpw",
            serverName = "www.example.com",
            insecure = true,
        )

        val bean = RawUpdater.parseSingBoxTrojan(outbound)
        assertEquals("Fastup fixture", bean.name)
        assertEquals("node.example", bean.serverAddress)
        assertEquals(443, bean.serverPort)
        assertEquals("synthetic-password#fastup", bean.password)
        assertEquals("rotated-mpw", bean.mpw)
        assertEquals("www.example.com", bean.sni)
        assertEquals(true, bean.allowInsecure)
    }

    @Test
    fun singBoxTrojanWithoutMpwStaysStandard() {
        val outbound = RawUpdater.SingBoxTrojanFields(
            tag = "Standard",
            server = "node.example",
            serverPort = 443,
            password = "ordinary",
            mpw = "",
        )

        val bean = RawUpdater.parseSingBoxTrojan(outbound)
        assertEquals("ordinary", bean.password)
        assertEquals("", bean.mpw)
    }
}
