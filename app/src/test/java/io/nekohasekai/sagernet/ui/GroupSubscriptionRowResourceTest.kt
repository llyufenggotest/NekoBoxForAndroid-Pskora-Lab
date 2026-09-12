package io.nekohasekai.sagernet.ui

import java.io.File
import javax.xml.parsers.DocumentBuilderFactory
import org.junit.Assert.*
import org.junit.Test

class GroupSubscriptionRowResourceTest {
    private fun resource(path: String): File = listOf(File("src/main/res/$path"), File("app/src/main/res/$path"))
        .first { it.isFile }
    private fun text(path: String) = resource(path).readText().replace("\r\n", "\n")

    @Test fun groupSubscriptionRowKeepsRoundedContainerWithOnlyTopAndBottomLines() {
        val xml = text("layout/layout_group_item.xml")
        assertTrue(xml.contains("app:cardCornerRadius="))
        assertTrue(xml.contains("@drawable/bg_group_subscription_row_lines"))
        val lines = text("drawable/bg_group_subscription_row_lines.xml")
        assertEquals(1, Regex("android:gravity=\"top\"").findAll(lines).count())
        assertEquals(1, Regex("android:gravity=\"bottom\"").findAll(lines).count())
        assertFalse(lines.contains("stroke"))
        DocumentBuilderFactory.newInstance().newDocumentBuilder().parse(resource("layout/layout_group_item.xml"))
        DocumentBuilderFactory.newInstance().newDocumentBuilder().parse(resource("drawable/bg_group_subscription_row_lines.xml"))
    }

    @Test fun groupRowTopActionsUseOutlineVectors() {
        val xml = text("layout/layout_group_item.xml")
        for (icon in listOf("ic_group_pin_outline", "ic_group_edit_outline", "ic_group_more_outline")) {
            assertTrue(xml.contains("@drawable/$icon"))
            val vector = text("drawable/$icon.xml")
            assertTrue(vector.contains("android:fillColor=\"@android:color/transparent\""))
            assertTrue(vector.contains("android:strokeColor="))
            DocumentBuilderFactory.newInstance().newDocumentBuilder().parse(resource("drawable/$icon.xml"))
        }
        val menu = text("menu/add_group_menu.xml")
        assertTrue(menu.contains("@drawable/ic_group_update_all_outline"))
        assertTrue(menu.contains("@drawable/ic_group_add_outline"))
    }
}
