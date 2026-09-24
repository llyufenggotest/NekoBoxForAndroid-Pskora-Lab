package io.nekohasekai.sagernet.ui

import android.view.View

/** Lightweight one-shot feedback; uses only ViewPropertyAnimator. */
internal fun View.playGreenPulse() {
    animate().cancel()
    scaleX = 1f
    scaleY = 1f
    alpha = 1f
    animate()
        .scaleX(1.18f)
        .scaleY(1.18f)
        .alpha(0.72f)
        .setDuration(110L)
        .withEndAction {
            animate()
                .scaleX(1f)
                .scaleY(1f)
                .alpha(1f)
                .setDuration(150L)
                .start()
        }
        .start()
}
