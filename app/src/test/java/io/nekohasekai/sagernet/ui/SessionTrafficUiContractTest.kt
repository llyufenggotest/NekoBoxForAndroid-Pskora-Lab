package io.nekohasekai.sagernet.ui

import java.io.File
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class SessionTrafficUiContractTest {
    private fun projectFile(path: String): String {
        val candidates = listOf(File(path), File("app/$path"))
        return candidates.firstOrNull { it.isFile }?.readText()
            ?: error("Cannot find project file: $path from ${File(".").absolutePath}")
    }

    @Test
    fun profileRowsDoNotContainCumulativeTrafficViewOrBinding() {
        val layout = projectFile("src/main/res/layout/layout_profile.xml")
        val fragment = projectFile("src/main/java/io/nekohasekai/sagernet/ui/ConfigurationFragment.kt")
        val holder = fragment.substringAfter("inner class ConfigurationHolder")

        assertFalse(layout.contains("traffic_text"))
        assertFalse(holder.contains("trafficText"))
        assertFalse(holder.contains("bindTraffic"))
        assertFalse(holder.contains("R.string.traffic"))
    }

    @Test
    fun connectionPanelOwnsSingleLineSessionTotals() {
        val layout = projectFile("src/main/res/layout/layout_group_list.xml")
        val fragment = projectFile("src/main/java/io/nekohasekai/sagernet/ui/ConfigurationFragment.kt")
        val activity = projectFile("src/main/java/io/nekohasekai/sagernet/ui/MainActivity.kt")

        assertTrue(layout.contains("@+id/dashboard_session_upload"))
        assertTrue(layout.contains("@+id/dashboard_session_download"))
        assertTrue(layout.contains("android:maxLines=\"1\""))
        assertFalse(layout.contains("@+id/dashboard_profile_name"))
        assertTrue(fragment.contains("targetProfileId: Long"))
        assertTrue(fragment.contains("txTotal: Long"))
        assertTrue(fragment.contains("rxTotal: Long"))
        assertTrue(layout.contains("本次上传"))
        assertTrue(layout.contains("本次下载"))
        assertTrue(fragment.contains("dashboardSessionUpload.text"))
        assertTrue(fragment.contains("dashboardSessionDownload.text"))
        assertTrue(activity.contains("stats.txTotal"))
        assertTrue(activity.contains("stats.rxTotal"))
    }
}
