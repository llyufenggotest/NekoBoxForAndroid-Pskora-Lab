package io.nekohasekai.sagernet.ui

import java.io.File
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class NightModeUiContractTest {
    private fun resource(path: String): String = listOf(
        File("src/main/res/$path"),
        File("app/src/main/res/$path")
    ).first { it.isFile }.readText().replace("\r\n", "\n")

    @Test fun addedSurfacesUseDayNightSemanticColors() {
        val files = listOf("layout/layout_appbar.xml", "layout/layout_group_list.xml",
            "layout/layout_main.xml", "layout/layout_share.xml")
        files.forEach { path ->
            val xml = resource(path)
            assertFalse("$path retains a hard-coded lab surface", xml.contains("@android:color/white"))
            listOf("#F1F4FA", "#FAFFFFFF", "#F7F8FB", "#CCFFFFFF", "#DDF5F7FC", "#EEF1F5FA")
                .forEach { assertFalse("$path retains $it", xml.contains(it)) }
        }
        val night = resource("values-night/colors.xml")
        listOf("lab_window_background", "lab_surface", "lab_surface_elevated", "lab_text_primary",
            "lab_text_secondary", "lab_bottom_surface", "lab_control_idle_background")
            .forEach { assertTrue("night color missing: $it", night.contains("name=\"$it\"")) }
    }

    @Test fun homeNodeRowsShareThePageBackgroundAndKeepOnlySeparators() {
        val profile = resource("layout/layout_profile.xml")
        assertTrue(profile.contains("app:cardBackgroundColor=\"@android:color/transparent\""))
        assertTrue(profile.contains("android:background=\"@color/lab_divider\""))
        assertFalse(profile.contains("?attr/colorSurface"))

        val fragment = listOf(
            File("src/main/java/io/nekohasekai/sagernet/ui/ConfigurationFragment.kt"),
            File("app/src/main/java/io/nekohasekai/sagernet/ui/ConfigurationFragment.kt")
        ).first { it.isFile }.readText().replace("\r\n", "\n")
        assertTrue(fragment.contains("ctx.getColorAttr(android.R.attr.colorBackground)"))
        assertTrue(fragment.contains("else Color.TRANSPARENT"))
    }

    @Test fun scrollingListsClearPersistentBottomControls() {
        val profile = resource("layout/layout_profile_list.xml")
        assertTrue(profile.contains("android:paddingBottom=\"156dp\""))
        assertTrue(profile.contains("android:clipToPadding=\"false\""))
        for (path in listOf("layout/layout_route.xml", "layout/layout_group.xml")) {
            val xml = resource(path)
            assertTrue("$path must scroll above bottom controls", xml.contains("android:paddingBottom=\"156dp\""))
            assertTrue("$path must render inside bottom padding", xml.contains("android:clipToPadding=\"false\""))
        }
    }
}

