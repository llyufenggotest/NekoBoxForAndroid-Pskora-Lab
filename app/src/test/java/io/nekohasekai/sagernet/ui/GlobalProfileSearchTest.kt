package io.nekohasekai.sagernet.ui

import org.junit.Assert.assertEquals
import org.junit.Test

class GlobalProfileSearchTest {

    private data class Profile(
        val id: Long,
        val groupId: Long,
        val name: String,
        val type: String,
        val address: String,
    )

    @Test
    fun nonEmptyQuerySearchesProfilesFromEveryGroup() {
        val currentGroupId = 1L
        val allProfiles = listOf(
            Profile(1, currentGroupId, "香港01-X365", "VLESS", "hk.example"),
            Profile(2, 2L, "台湾01-X365", "VLESS", "tw.example"),
        )

        val result = selectProfilesForQuery(
            query = "台湾",
            currentGroupId = currentGroupId,
            allProfiles = allProfiles,
            groupId = Profile::groupId,
            matches = { profile, query ->
                profile.name.contains(query, ignoreCase = true) ||
                    profile.type.contains(query, ignoreCase = true) ||
                    profile.address.contains(query, ignoreCase = true)
            },
        )

        assertEquals(listOf(2L), result.map(Profile::id))
    }

    @Test
    fun emptyQueryReturnsOnlyCurrentGroupProfiles() {
        val currentGroupId = 1L
        val allProfiles = listOf(
            Profile(1, currentGroupId, "香港01-X365", "VLESS", "hk.example"),
            Profile(2, 2L, "台湾01-X365", "VLESS", "tw.example"),
        )

        val result = selectProfilesForQuery(
            query = "",
            currentGroupId = currentGroupId,
            allProfiles = allProfiles,
            groupId = Profile::groupId,
            matches = { _, _ -> true },
        )

        assertEquals(listOf(1L), result.map(Profile::id))
    }
}
