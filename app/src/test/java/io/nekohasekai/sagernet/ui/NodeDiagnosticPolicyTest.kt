package io.nekohasekai.sagernet.ui

import io.nekohasekai.sagernet.bg.BaseService
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class NodeDiagnosticPolicyTest {
    @Test fun latencyOnlyWorksWhileStopped() {
        assertTrue(nodeDiagnosticActions(BaseService.State.Stopped, 7, 7, 0).latencyEnabled)
        assertFalse(nodeDiagnosticActions(BaseService.State.Connecting, 7, 7, 0).latencyEnabled)
        assertFalse(nodeDiagnosticActions(BaseService.State.Connected, 7, 7, 7).latencyEnabled)
        assertFalse(nodeDiagnosticActions(BaseService.State.Stopping, 7, 7, 0).latencyEnabled)
    }

    @Test fun qualityAndSpeedRequireConnectedSelectedActiveNode() {
        val active = nodeDiagnosticActions(BaseService.State.Connected, 7, 7, 7)
        assertTrue(active.qualityVisible)
        assertTrue(active.speedVisible)
        val inactive = nodeDiagnosticActions(BaseService.State.Connected, 8, 7, 7)
        assertFalse(inactive.qualityVisible)
        assertFalse(inactive.speedVisible)
    }

    @Test fun currentRuntimeNodeRemainsActiveWhenSelectionSnapshotLags() {
        val active = nodeDiagnosticActions(BaseService.State.Connected, 7, 8, 7)
        assertTrue(active.qualityVisible)
        assertTrue(active.speedVisible)
    }

    @Test fun preferredRuntimeLeafCanOwnConnectedActions() {
        val active = nodeDiagnosticActions(
            BaseService.State.Connected,
            rowProfileId = 9,
            selectedProfileId = 9,
            currentProfileId = 100,
            activePreferredLeafId = 9,
        )
        assertTrue(active.qualityVisible)
        assertTrue(active.speedVisible)
    }
}
