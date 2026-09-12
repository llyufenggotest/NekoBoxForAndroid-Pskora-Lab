package io.nekohasekai.sagernet.ui

import org.junit.Assert.assertEquals
import org.junit.Test

class GroupPinOrderTest {

    private data class Group(
        val id: Long,
        var userOrder: Long,
    )

    @Test
    fun latestPinnedGroupComesFirstAndEarlierPinsFollow() {
        val groups = mutableListOf(
            Group(1, 1),
            Group(2, 2),
            Group(3, 3),
        )

        moveItemToFrontAndReindex(groups, fromIndex = 1) { group, order ->
            group.userOrder = order
        }
        assertEquals(listOf(2L, 1L, 3L), groups.map(Group::id))
        assertEquals(listOf(1L, 2L, 3L), groups.map(Group::userOrder))

        moveItemToFrontAndReindex(groups, fromIndex = 2) { group, order ->
            group.userOrder = order
        }
        assertEquals(listOf(3L, 2L, 1L), groups.map(Group::id))
        assertEquals(listOf(1L, 2L, 3L), groups.map(Group::userOrder))
    }

    @Test
    fun pinningFirstGroupKeepsStableOrder() {
        val groups = mutableListOf(
            Group(1, 1),
            Group(2, 2),
        )

        moveItemToFrontAndReindex(groups, fromIndex = 0) { group, order ->
            group.userOrder = order
        }

        assertEquals(listOf(1L, 2L), groups.map(Group::id))
        assertEquals(listOf(1L, 2L), groups.map(Group::userOrder))
    }
}
