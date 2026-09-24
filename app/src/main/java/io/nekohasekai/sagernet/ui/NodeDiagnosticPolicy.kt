package io.nekohasekai.sagernet.ui

import io.nekohasekai.sagernet.bg.BaseService

internal data class NodeDiagnosticActions(
    val latencyEnabled: Boolean,
    val qualityVisible: Boolean,
    val speedVisible: Boolean,
)

internal fun nodeDiagnosticActions(
    state: BaseService.State,
    rowProfileId: Long,
    selectedProfileId: Long,
    currentProfileId: Long,
    activePreferredLeafId: Long = 0L,
): NodeDiagnosticActions {
    val stopped = state == BaseService.State.Stopped || state == BaseService.State.Idle
    val connected = state == BaseService.State.Connected
    val selected = rowProfileId > 0L && rowProfileId == selectedProfileId
    val active = connected && selected &&
        (rowProfileId == currentProfileId || rowProfileId == activePreferredLeafId)
    return NodeDiagnosticActions(
        latencyEnabled = stopped,
        qualityVisible = active,
        speedVisible = active,
    )
}
