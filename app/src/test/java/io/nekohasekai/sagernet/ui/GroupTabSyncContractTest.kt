package io.nekohasekai.sagernet.ui

import java.io.File
import org.junit.Assert.assertTrue
import org.junit.Test

class GroupTabSyncContractTest {
    @Test
    fun configurationPreservesVisibleGroupDuringReloadAndSupportsDoubleTap() {
        val file = listOf(
            File("src/main/java/io/nekohasekai/sagernet/ui/ConfigurationFragment.kt"),
            File("app/src/main/java/io/nekohasekai/sagernet/ui/ConfigurationFragment.kt")
        ).first { it.isFile }
        val source = file.readText()
        assertTrue(source.contains("selectedGroupIndex = position"))
        assertTrue(source.contains("val visibleGroupId = groupList.getOrNull(selectedGroupIndex)?.id"))
        assertTrue(source.contains("var selectedGroup = visibleGroupId"))
        assertTrue(source.contains("lastGroupTabTapAt"))
        assertTrue(source.contains("groupTabDoubleTapTarget("))
        assertTrue(source.contains("if (adapter.groupList.isEmpty()) return@setOnTouchListener true"))
        assertTrue(source.contains("event.rawX"))
        assertTrue(source.contains("resources.displayMetrics.widthPixels"))
        assertTrue(source.contains("groupPager.setCurrentItem(targetIndex, false)"))
        assertTrue(source.contains("tabLayout.setScrollPosition(targetIndex, 0f, true)"))
        assertTrue(source.contains("override fun isLongPressDragEnabled(): Boolean = false"))
        assertTrue(source.contains("itemTouchHelper.startDrag(holder)"))
        assertTrue(source.contains("if (::itemTouchHelper.isInitialized && isEnabled"))
        assertTrue(source.contains("armManualDrag(view, this)"))
    }
}
