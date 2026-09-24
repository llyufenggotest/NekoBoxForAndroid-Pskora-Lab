package io.nekohasekai.sagernet.ui

import java.io.File
import javax.xml.parsers.DocumentBuilderFactory
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.w3c.dom.Element

class ProfileCardQualityResourcesContractTest {
    private fun projectFile(path: String): File = listOf(File(path), File("app/$path"))
        .firstOrNull { it.isFile } ?: error("Cannot find project file: $path")

    private fun text(path: String) = projectFile(path).readText().replace("\r\n", "\n")

    private fun elements(path: String): List<Element> {
        val document = DocumentBuilderFactory.newInstance().apply { isNamespaceAware = true }
            .newDocumentBuilder().parse(projectFile(path))
        val all = document.getElementsByTagName("*")
        return (0 until all.length).map { all.item(it) as Element }
    }

    private fun Element.android(name: String) =
        getAttributeNS("http://schemas.android.com/apk/res/android", name)

    @Test
    fun sharedProfileCardPreservesBindingIdsAndAddsCompactMetadata() {
        val nodes = elements("src/main/res/layout/layout_profile.xml")
        val byId = nodes.filter { it.android("id").isNotEmpty() }
            .associateBy { it.android("id").substringAfterLast('/') }

        listOf("content", "content_lin", "container", "profile_name", "edit",
            "double_column_menu", "share", "share_layer", "shareIcon", "remove",
            "profile_address", "profile_type", "profile_status", "selected_indicator")
            .forEach { assertNotNull("missing existing id $it", byId[it]) }

        listOf("profile_preferred_group", "profile_latency", "profile_upload_speed",
            "profile_download_speed", "profile_leaf", "profile_speedometer",
            "profile_lightning").forEach { assertNotNull("missing new id $it", byId[it]) }

        assertEquals("@style/LabProfileMetadata", byId.getValue("profile_type").android("textAppearance"))
        assertEquals("@style/LabProfileMetadata", byId.getValue("profile_latency").android("textAppearance"))
        assertEquals("@string/profile_preferred_group_description", byId.getValue("profile_preferred_group").android("contentDescription"))
        assertEquals("@string/profile_latency_description", byId.getValue("profile_latency").android("contentDescription"))
    }

    @Test
    fun compactActionsKeepAccessibleTouchTargetsAndSmallerGlyphs() {
        val nodes = elements("src/main/res/layout/layout_profile.xml")
        val byId = nodes.filter { it.android("id").isNotEmpty() }
            .associateBy { it.android("id").substringAfterLast('/') }

        listOf("edit", "share", "remove", "profile_leaf", "profile_speedometer", "profile_lightning")
            .forEach { id ->
                val view = byId.getValue(id)
                assertEquals("$id touch width", "48dp", view.android("layout_width"))
                assertEquals("$id touch height", "48dp", view.android("layout_height"))
                assertTrue("$id needs a description", view.android("contentDescription").startsWith("@string/"))
            }
        listOf("edit", "remove").forEach { id -> assertEquals("14dp", byId.getValue(id).android("padding")) }
        assertEquals("20dp", byId.getValue("shareIcon").android("layout_width"))
        assertEquals("20dp", byId.getValue("shareIcon").android("layout_height"))
    }

    @Test
    fun metadataAndTrafficCanShrinkInsideTwoColumnCards() {
        val layout = text("src/main/res/layout/layout_profile.xml")
        assertTrue(layout.contains("android:id=\"@+id/profile_metadata_row\""))
        assertTrue(layout.contains("android:id=\"@+id/profile_traffic_row\""))
        assertTrue(layout.contains("android:id=\"@+id/profile_upload_speed\""))
        assertTrue(layout.contains("android:id=\"@+id/profile_download_speed\""))
        assertTrue(layout.contains("android:ellipsize=\"end\""))
        assertTrue(layout.contains("android:layout_width=\"0dp\""))
        assertFalse(layout.contains("android:minWidth=\"360dp\""))
    }

    @Test
    fun qualityAssetsStylesStringsAndNightColorsExist() {
        val dialogLayout = text("src/main/res/layout/dialog_ip_quality.xml")
        listOf("ip_quality_dialog_title", "ip_quality_dialog_summary",
            "ip_quality_dialog_content", "ip_quality_dialog_close").forEach { id ->
            assertTrue("missing dialog id $id", dialogLayout.contains("@+id/$id"))
        }
        assertTrue(dialogLayout.contains("android:layout_width=\"48dp\""))
        assertTrue(dialogLayout.contains("android:layout_height=\"48dp\""))
        assertTrue(dialogLayout.contains("@string/ip_quality_dialog_close"))

        listOf("ic_profile_leaf_outline.xml", "ic_profile_speedometer_outline.xml",
            "ic_profile_lightning_outline.xml").forEach { file ->
            val vector = text("src/main/res/drawable/$file")
            assertTrue(vector.contains("<vector"))
            assertTrue(vector.contains("android:tint=\"@color/profile_card_icon\""))
        }
        val dialog = text("src/main/res/drawable/bg_ip_quality_dialog.xml")
        assertTrue(dialog.contains("android:radius=\"24dp\""))
        assertTrue(dialog.contains("@color/ip_quality_dialog_surface"))

        val styles = text("src/main/res/values/lab_styles.xml")
        assertTrue(styles.contains("name=\"LabProfileMetadata\""))
        assertTrue(styles.contains("name=\"ThemeOverlay.SagerNet.IpQualityDialog\""))
        assertTrue(styles.contains("@drawable/bg_ip_quality_dialog"))

        val strings = text("src/main/res/values/strings.xml")
        listOf("profile_preferred_group_description", "profile_latency_description",
            "profile_upload_speed_description", "profile_download_speed_description",
            "profile_leaf_description", "profile_speedometer_description",
            "profile_lightning_description", "ip_quality_dialog_title")
            .forEach { assertTrue("missing string $it", strings.contains("name=\"$it\"")) }

        val day = text("src/main/res/values/colors.xml")
        val night = text("src/main/res/values-night/colors.xml")
        listOf("profile_card_icon", "profile_card_accent", "ip_quality_dialog_surface",
            "ip_quality_dialog_separator").forEach { color ->
            assertTrue("missing day color $color", day.contains("name=\"$color\""))
            assertTrue("missing night color $color", night.contains("name=\"$color\""))
        }
    }
}
