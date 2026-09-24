package io.nekohasekai.sagernet.ui

import org.junit.Assert.assertEquals
import org.junit.Test

class GroupTabDoubleTapTargetTest {
    @Test fun leftHalfTargetsFirstAndRightHalfTargetsLast() {
        assertEquals(0, groupTabDoubleTapTarget(rawX = 539f, screenWidth = 1080, lastIndex = 6))
        assertEquals(6, groupTabDoubleTapTarget(rawX = 540f, screenWidth = 1080, lastIndex = 6))
        assertEquals(6, groupTabDoubleTapTarget(rawX = 1000f, screenWidth = 1080, lastIndex = 6))
    }

    @Test fun emptyListFallsBackToZero() {
        assertEquals(0, groupTabDoubleTapTarget(rawX = 900f, screenWidth = 1080, lastIndex = -1))
    }
}
