package io.nekohasekai.sagernet.ui

import java.io.File
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class NodeActionVisualContractTest {
    private fun source(path: String): String = listOf(File(path), File("app/$path"))
        .first { it.isFile }.readText()

    @Test fun doubleColumnMenusUseSharedUpwardPopup() {
        val configuration = source("src/main/java/io/nekohasekai/sagernet/ui/ConfigurationFragment.kt")
        val preferred = source("src/main/java/io/nekohasekai/sagernet/ui/PreferredGroupFragment.kt")
        assertTrue(configuration.contains("showUpwardActionMenu("))
        assertTrue(preferred.contains("showUpwardActionMenu("))
        assertFalse(configuration.substringAfter("private fun showDoubleColumnMenu").substringBefore("private fun applySelected").contains("PopupMenu("))
        assertFalse(preferred.substringAfter("private fun showDoubleColumnMenu").substringBefore("private fun sourceAction").contains("PopupMenu("))
    }

    @Test fun lightningStaysFullyVisibleInEveryState() {
        val configuration = source("src/main/java/io/nekohasekai/sagernet/ui/ConfigurationFragment.kt")
        val preferred = source("src/main/java/io/nekohasekai/sagernet/ui/PreferredGroupFragment.kt")
        assertTrue(configuration.contains("lightningButton.alpha = 1f"))
        assertTrue(preferred.contains("holder.lightning.alpha = 1f"))
        assertTrue(configuration.contains("getColorAttr(android.R.attr.textColorPrimary)"))
        assertTrue(preferred.contains("getColorAttr(android.R.attr.textColorPrimary)"))
    }

    @Test fun latencyActionDoesNotRequireHistoricalPing() {
        val configuration = source("src/main/java/io/nekohasekai/sagernet/ui/ConfigurationFragment.kt")
        val action = configuration.substringAfter("private fun testNodeLatency(profile: ProxyEntity,")
            .substringBefore("fun startSpeedTestForProfile")
        assertTrue(action.contains("runNodeLatency(profile, mode)"))
        assertFalse(action.contains("profile.ping >"))
        assertFalse(action.contains("profile.status == 1"))
        assertFalse(action.contains("profile.status = 0"))
        assertTrue(action.contains("val previousStatus = profile.status"))
        assertTrue(action.contains("profile.status = previousStatus"))
        assertTrue(action.contains("nodeLatencyTickets[profile.id] != ticket"))
        assertTrue(configuration.contains("configuration?.testNodeLatency(entity, nodeLatencyMode(longPress = false))"))
        assertTrue(configuration.contains("configuration?.testNodeLatency(entity, nodeLatencyMode(longPress = true))"))
        assertTrue(configuration.contains("lightningButton.playGreenPulse()"))
        val preferred = source("src/main/java/io/nekohasekai/sagernet/ui/PreferredGroupFragment.kt")
        assertTrue(preferred.contains("testSingleMember(it, nodeLatencyMode(longPress = false))"))
        assertTrue(preferred.contains("testSingleMember(it, nodeLatencyMode(longPress = true))"))
        assertTrue(preferred.contains("holder.lightning.playGreenPulse()"))
    }
}