package io.nekohasekai.sagernet.ui

import org.junit.Assert.assertEquals
import org.junit.Test

class NodeLatencyGestureTest {
    @Test fun tapUsesUrlTestAndLongPressUsesTcp() {
        assertEquals(NodeLatencyMode.URL_TEST, nodeLatencyMode(longPress = false))
        assertEquals(NodeLatencyMode.TCP, nodeLatencyMode(longPress = true))
    }
}
