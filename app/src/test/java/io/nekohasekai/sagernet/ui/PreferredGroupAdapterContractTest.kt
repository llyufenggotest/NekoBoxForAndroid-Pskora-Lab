package io.nekohasekai.sagernet.ui

import java.io.File
import org.junit.Assert.*
import org.junit.Test

class PreferredGroupAdapterContractTest {
    private fun source(name: String): String = listOf(
        File("src/main/java/io/nekohasekai/sagernet/ui/$name.kt"),
        File("app/src/main/java/io/nekohasekai/sagernet/ui/$name.kt")
    ).first { it.isFile }.readText().replace("\r\n", "\n")

    @Test fun adapterUsesSourceIdsAndDiffsReorderedRows() {
        val fragment = source("PreferredGroupFragment")
        assertTrue(fragment.contains("setHasStableIds(true)"))
        assertTrue(fragment.contains("override fun getItemId(position: Int)"))
        assertTrue(fragment.contains("rows[position].first?.id"))
        assertTrue(fragment.contains("DiffUtil.calculateDiff"))
        assertTrue(fragment.contains("}, false)"))
        assertTrue(fragment.contains("cards.notifyItemRangeChanged"))
        assertFalse(fragment.contains("cards.notifyDataSetChanged()"))
    }
}
