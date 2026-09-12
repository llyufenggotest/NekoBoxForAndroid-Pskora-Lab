package io.nekohasekai.sagernet.bg

import moe.matsuri.nb4a.utils.SendLog
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

class VpnUnderlyingExportLogContractTest {
    @Test fun emittedMarkerIsReadBackFromTheSameNekoLogFileUsedByExport() {
        val exportedNekoLog = File.createTempFile("vpn-underlying-export-", ".log")
        try {
            val sink = VpnUnderlyingDiagnosticSink { line ->
                exportedNekoLog.appendText("$line\n")
            }
            val marker = VpnUnderlyingDiagnosticModel().recordSetRequest(
                requestedHash = 11L,
                result = VpnUnderlyingDiagnosticModel.Result.TRUE,
                threadHash = 7L,
            ).render()

            sink.write(marker)

            val exported = SendLog.readNekoLog(exportedNekoLog, 0).toString(Charsets.UTF_8)
            assertTrue(exported.contains(marker))
            assertTrue(exported.contains("VPNUNDERTRACE schema=2"))
            assertFalse(exported.contains("android.util.Log"))
        } finally {
            exportedNekoLog.delete()
        }
    }

    @Test fun exportTailContractStillReturnsTheRequestedSuffix() {
        val exportedNekoLog = File.createTempFile("vpn-underlying-tail-", ".log")
        try {
            exportedNekoLog.writeText("old-line\nVPNUNDERTRACE schema=2 seq=1 kind=1\n")
            val tail = SendLog.readNekoLog(exportedNekoLog, 24)
            assertEquals(exportedNekoLog.readBytes().takeLast(24).toByteArray().toList(), tail.toList())
        } finally {
            exportedNekoLog.delete()
        }
    }
}
