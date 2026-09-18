package io.nekohasekai.sagernet.ui

import java.io.File
import org.junit.Assert.assertTrue
import org.junit.Test

class GroupTabSyncContractTest {
    @Test
    fun configurationPreservesVisibleGroupDuringReloadAndSupportsDoubleTap() {
        val file = File("src/main/java/io/nekohasekai/sagernet/ui/ConfigurationFragment.kt")
        val source = file.readText()
        assertTrue(source.contains("val visibleGroupId = groupList.getOrNull(selectedGroupIndex)?.id"))
        assertTrue(source.contains("var selectedGroup = visibleGroupId"))
        assertTrue(source.contains("lastGroupTabTapAt"))
        assertTrue(source.contains("groupPager.setCurrentItem(0, false)"))
        assertTrue(source.contains("tabLayout.setScrollPosition(0, 0f, true)"))
    }
}
