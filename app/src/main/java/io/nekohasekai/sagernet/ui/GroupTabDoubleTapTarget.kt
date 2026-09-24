package io.nekohasekai.sagernet.ui

internal fun groupTabDoubleTapTarget(
    rawX: Float,
    screenWidth: Int,
    lastIndex: Int,
): Int = if (rawX < screenWidth / 2f) 0 else lastIndex.coerceAtLeast(0)
