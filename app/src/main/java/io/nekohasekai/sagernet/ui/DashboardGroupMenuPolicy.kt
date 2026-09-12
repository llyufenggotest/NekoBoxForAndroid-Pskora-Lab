package io.nekohasekai.sagernet.ui

data class DashboardGroupMenuCapabilities(
    val canEdit: Boolean,
    val canCopySubscriptionLink: Boolean,
    val canDelete: Boolean,
)

fun dashboardGroupMenuCapabilities(
    ungrouped: Boolean,
    isSubscription: Boolean,
    subscriptionUrlPresent: Boolean,
    groupCount: Int,
    isUpdating: Boolean,
): DashboardGroupMenuCapabilities = DashboardGroupMenuCapabilities(
    canEdit = !ungrouped && !isUpdating,
    canCopySubscriptionLink = isSubscription && subscriptionUrlPresent,
    canDelete = !ungrouped && !isUpdating && groupCount > 1,
)

fun fallbackGroupIdAfterDelete(groupIds: List<Long>, deletedIndex: Int): Long? {
    if (deletedIndex !in groupIds.indices || groupIds.size <= 1) return null
    return when {
        deletedIndex + 1 < groupIds.size -> groupIds[deletedIndex + 1]
        deletedIndex > 0 -> groupIds[deletedIndex - 1]
        else -> null
    }
}
