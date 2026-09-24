package io.nekohasekai.sagernet.ui

import org.junit.Assert.assertEquals
import org.junit.Test

class NodeSpeedTestGestureTest {
    @Test fun tapUsesEightStreamsAndLongPressUsesOne() {
        assertEquals(8, nodeSpeedTestStreams(longPress = false))
        assertEquals(1, nodeSpeedTestStreams(longPress = true))
    }
}
