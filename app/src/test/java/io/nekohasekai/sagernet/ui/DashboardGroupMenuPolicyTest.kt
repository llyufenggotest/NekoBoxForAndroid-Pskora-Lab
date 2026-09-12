package io.nekohasekai.sagernet.ui

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class DashboardGroupMenuPolicyTest {
    @Test fun subscriptionGroupEnablesAllApplicableActions() {
        val result = dashboardGroupMenuCapabilities(false, true, true, 3, false)
        assertTrue(result.canEdit)
        assertTrue(result.canCopySubscriptionLink)
        assertTrue(result.canDelete)
    }

    @Test fun localAndBuiltInGroupsHideInapplicableActions() {
        val local = dashboardGroupMenuCapabilities(false, false, false, 3, false)
        assertTrue(local.canEdit)
        assertFalse(local.canCopySubscriptionLink)

        val builtIn = dashboardGroupMenuCapabilities(true, false, false, 3, false)
        assertFalse(builtIn.canEdit)
        assertFalse(builtIn.canCopySubscriptionLink)
        assertFalse(builtIn.canDelete)
    }

    @Test fun soleOrUpdatingGroupCannotBeDeleted() {
        assertFalse(dashboardGroupMenuCapabilities(false, true, true, 1, false).canDelete)
        assertFalse(dashboardGroupMenuCapabilities(false, true, true, 3, true).canDelete)
    }

    @Test fun deletionFallsForwardThenBackwardWithoutDanglingSelection() {
        assertEquals(30L, fallbackGroupIdAfterDelete(listOf(10L, 20L, 30L), 1))
        assertEquals(20L, fallbackGroupIdAfterDelete(listOf(10L, 20L, 30L), 2))
        assertNull(fallbackGroupIdAfterDelete(listOf(10L), 0))
        assertNull(fallbackGroupIdAfterDelete(listOf(10L, 20L), 3))
    }
}
