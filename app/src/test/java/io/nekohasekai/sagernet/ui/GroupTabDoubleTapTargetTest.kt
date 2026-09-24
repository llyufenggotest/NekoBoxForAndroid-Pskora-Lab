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

    @Test fun doubleTapFollowsPhysicalFingerEvenWhenSelectedTabRecenters() {
        assertEquals(true, groupTabDoubleTapMatches(
            lastTapAt = 1_000L,
            now = 1_220L,
            lastRawX = 720f,
            rawX = 724f,
            doubleTapSlop = 100,
        ))
    }

    @Test fun distantOrLateTapDoesNotCountAsDoubleTap() {
        assertEquals(false, groupTabDoubleTapMatches(1_000L, 1_220L, 300f, 720f, 100))
        assertEquals(false, groupTabDoubleTapMatches(1_000L, 1_500L, 720f, 724f, 100))
    }
}
