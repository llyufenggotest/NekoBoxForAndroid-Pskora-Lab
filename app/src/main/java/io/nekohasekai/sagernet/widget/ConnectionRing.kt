package io.nekohasekai.sagernet.widget

import android.animation.ValueAnimator
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.ColorFilter
import android.graphics.Paint
import android.graphics.PixelFormat
import android.graphics.drawable.Drawable
import android.view.View
import androidx.lifecycle.DefaultLifecycleObserver
import androidx.lifecycle.LifecycleOwner

/** Overlay-only ring: never changes the button's alpha, scale or hit bounds. */
class ConnectionRing(private val view: View, private val owner: LifecycleOwner) : Drawable(), DefaultLifecycleObserver, View.OnAttachStateChangeListener {
    private val paint = Paint(Paint.ANTI_ALIAS_FLAG)
    private val dp = view.resources.displayMetrics.density
    private var animator: ValueAnimator? = null
    private var connected = false
    private var active = owner.lifecycle.currentState.isAtLeast(androidx.lifecycle.Lifecycle.State.STARTED)
    private var strength = 0f
    private val layoutListener = View.OnLayoutChangeListener { _, _, _, _, _, _, _, _, _ ->
        setBounds(0, 0, view.width, view.height)
        sync()
    }
    init {
        owner.lifecycle.addObserver(this)
        view.addOnAttachStateChangeListener(this)
        setBounds(0, 0, view.width, view.height)
        view.addOnLayoutChangeListener(layoutListener)
        view.overlay.add(this)
    }
    fun setConnected(value: Boolean) { connected = value; sync() }
    fun sync() {
        if (connected && active && view.isAttachedToWindow && view.isShown) {
            if (animator != null) return
            animator = ValueAnimator.ofFloat(0f, 1f, 0f).apply {
                duration = 2400L
                repeatCount = ValueAnimator.INFINITE
                addUpdateListener { strength = it.animatedValue as Float; invalidateSelf() }
                start()
            }
        } else {
            animator?.cancel(); animator = null; strength = 0f; invalidateSelf()
        }
    }
    override fun draw(canvas: Canvas) {
        if (!connected || !active) return
        val radius = minOf(view.width, view.height) / 2f - 4f * dp
        if (radius <= 0) return
        paint.style = Paint.Style.STROKE
        paint.color = Color.rgb(32, 156, 105)
        // Concentric translucent strokes render a soft halo without software layers.
        for (i in 4 downTo 1) {
            paint.strokeWidth = (1.5f + i * 1.4f) * dp
            paint.alpha = ((5 + strength * 12) / i).toInt()
            canvas.drawCircle(view.width / 2f, view.height / 2f, radius, paint)
        }
        paint.strokeWidth = 1.5f * dp
        paint.alpha = 210
        canvas.drawCircle(view.width / 2f, view.height / 2f, radius, paint)
    }
    override fun onStart(owner: LifecycleOwner) { active = true; sync() }
    override fun onStop(owner: LifecycleOwner) { active = false; sync() }
    override fun onDestroy(owner: LifecycleOwner) = dispose()
    override fun onViewAttachedToWindow(v: View) = sync()
    override fun onViewDetachedFromWindow(v: View) { animator?.cancel(); animator = null }
    fun dispose() {
        animator?.cancel(); animator = null
        view.overlay.remove(this)
        view.removeOnLayoutChangeListener(layoutListener)
        view.removeOnAttachStateChangeListener(this)
        owner.lifecycle.removeObserver(this)
    }
    override fun setAlpha(alpha: Int) {}
    override fun setColorFilter(colorFilter: ColorFilter?) {}
    @Deprecated("Deprecated in Java") override fun getOpacity() = PixelFormat.TRANSLUCENT
}
