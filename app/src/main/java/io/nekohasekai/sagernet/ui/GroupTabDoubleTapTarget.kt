package io.nekohasekai.sagernet.ui

internal fun groupTabDoubleTapTarget(
    rawX: Float,
    screenWidth: Int,
    lastIndex: Int,
): Int = if (rawX < screenWidth / 2f) 0 else lastIndex.coerceAtLeast(0)

internal fun groupTabDoubleTapMatches(
    lastTapAt: Long,
    now: Long,
    lastRawX: Float,
    rawX: Float,
    doubleTapSlop: Int,
): Boolean = now - lastTapAt in 1..350 && kotlin.math.abs(rawX - lastRawX) <= doubleTapSlop
