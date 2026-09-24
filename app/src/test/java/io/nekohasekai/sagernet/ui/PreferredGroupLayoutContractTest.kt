package io.nekohasekai.sagernet.ui

import java.io.File
import org.junit.Assert.*
import org.junit.Test

class PreferredGroupLayoutContractTest {
    private fun projectFile(path: String): String = listOf(
        File(path),
        File("app/$path")
    ).first { it.isFile }.readText().replace("\r\n", "\n")

    @Test fun preferredListReusesOrdinaryLayoutPreferenceAndSpanLogic() {
        val fragment = projectFile("src/main/java/io/nekohasekai/sagernet/ui/PreferredGroupFragment.kt")
        assertTrue(fragment.contains("DataStore.groupLayoutMode == 1"))
        assertTrue(fragment.contains("FixedGridLayoutManager(recyclerView, 2)"))
        assertTrue(fragment.contains("FixedLinearLayoutManager(recyclerView)"))
        assertTrue(fragment.contains("fun switchLayoutMode()"))
    }

    @Test fun ordinaryLayoutSwitchAlsoUpdatesPreferredFragments() {
        val configuration = projectFile("src/main/java/io/nekohasekai/sagernet/ui/ConfigurationFragment.kt")
        assertTrue(configuration.contains("preferredGroupFragments.values.forEach"))
        assertTrue(configuration.contains("fragment.switchLayoutMode()"))
        assertTrue(configuration.contains("preferredGroupFragments[groupList[position].id] = fragment"))
    }

    @Test fun preferredRowsKeepSharedProfileLayoutAndCompactDoubleColumnActions() {
        val fragment = projectFile("src/main/java/io/nekohasekai/sagernet/ui/PreferredGroupFragment.kt")
        assertTrue(fragment.contains("inflate(R.layout.layout_profile"))
        assertTrue(fragment.contains("holder.bindLayoutMode("))
        assertTrue(fragment.contains("doubleColumn = isDoubleColumn()"))
        assertTrue(fragment.contains("showUpwardActionMenu("))
        assertTrue(fragment.contains("UpwardAction(R.id.action_edit"))
        assertTrue(fragment.contains("R.id.action_edit -> sourceAction(node, R.id.edit)"))
        assertTrue(fragment.contains("R.id.action_share -> sourceAction(node, R.id.share)"))
        assertTrue(fragment.contains("R.id.action_delete -> sourceAction(node, R.id.remove)"))
    }

    @Test fun preferredListKeepsBottomInsetAndNightSafeSharedResources() {
        val fragment = projectFile("src/main/java/io/nekohasekai/sagernet/ui/PreferredGroupFragment.kt")
        assertTrue(fragment.contains("setPadding(horizontalPadding, topPadding, horizontalPadding, bottomPadding)"))
        assertTrue(fragment.contains("dp(156)"))
        assertTrue(fragment.contains("clipToPadding = false"))
        assertFalse(fragment.contains("Color.WHITE"))
        assertFalse(fragment.contains("Color.BLACK"))
    }
}
