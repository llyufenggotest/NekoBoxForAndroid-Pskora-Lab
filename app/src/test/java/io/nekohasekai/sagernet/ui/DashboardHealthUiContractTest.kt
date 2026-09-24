package io.nekohasekai.sagernet.ui

import java.io.File
import org.junit.Assert.*
import org.junit.Test

class DashboardHealthUiContractTest {
    private fun source(name: String): String = listOf(
        File("src/main/java/io/nekohasekai/sagernet/ui/$name.kt"),
        File("app/src/main/java/io/nekohasekai/sagernet/ui/$name.kt")
    ).first { it.isFile }.readText().replace("\r\n", "\n")

    @Test fun serviceConnectionAndProfileChangesAutomaticallyProbe() {
        val activity = source("MainActivity")
        assertTrue(activity.contains("state == BaseService.State.Connected && previousState != state"))
        assertTrue(activity.contains("binding.root.post { runDashboardConnectionTest() }"))
        assertTrue(activity.contains("if (DataStore.serviceState.connected && dashboardHealth.result == null) runDashboardConnectionTest()"))
        assertTrue(activity.contains("!dashboardHealth.complete(ticket, result)"))
    }

    @Test fun blockingProbeCannotBeRepeatedAfterUiTimeout() {
        val activity = source("MainActivity")
        assertTrue(activity.contains("if (dashboardProbeInFlight)"))
        assertTrue(activity.contains("dashboardProbeInFlight = true"))
        assertTrue(activity.contains("finally {\n                dashboardProbeInFlight = false"))
    }

    @Test fun bothSurfacesReadSameHealthAndHomeRestoresResults() {
        val activity = source("MainActivity")
        val fragment = source("ConfigurationFragment")
        assertTrue(activity.contains("val tone = dashboardHealth.tone"))
        assertTrue(fragment.contains("(activity as? MainActivity)?.dashboardHealth"))
        assertTrue(fragment.contains("updateDashboardConnectionTest(profileId, health?.result, refreshState = false)"))
        assertFalse(fragment.contains("Color.parseColor(if (connected)"))
    }

    @Test fun ipQualityRecoversDisconnectedForegroundServiceBinding() {
        val activity = source("MainActivity")
        val fragment = source("ConfigurationFragment")
        assertTrue(activity.contains("fun reconnectServiceBinding()"))
        assertTrue(activity.contains("override fun onServiceDisconnected()"))
        assertTrue(activity.contains("reconnectServiceBinding()"))
        assertTrue(fragment.contains("bindDiagnosticService()"))
        assertTrue(fragment.contains("private suspend fun awaitDiagnosticService"))
        assertFalse(fragment.contains("mainActivity.reconnectServiceBinding()"))
        assertTrue(fragment.contains("SagerConnection.serviceClass"))
        assertTrue(fragment.contains("binding.service.queryIpQuality(profileId)"))
        assertTrue(fragment.contains("binding.release()"))
        assertFalse(fragment.contains("DataStore.serviceState.connected) return null"))
        assertFalse(fragment.contains("service()?.queryIpQuality(profileId) ?: error(\"Service disconnected\")"))
    }

    @Test fun updateShortcutUsesExistingUpdaterWithoutReplacingOtherActions() {
        val fragment = source("ConfigurationFragment")
        assertTrue(fragment.contains("GroupUpdater.executeUpdate(group, true)"))
        assertTrue(fragment.contains("group.id in GroupUpdater.updating || group.id in pendingDashboardUpdates"))
        for (label in listOf("更新订阅", "更新中…", "复制订阅链接", "置顶")) assertTrue(fragment.contains(label))
        assertTrue(fragment.contains("addAction(getString(R.string.edit)"))
        assertTrue(fragment.contains("addAction(getString(R.string.delete)"))
    }
}
