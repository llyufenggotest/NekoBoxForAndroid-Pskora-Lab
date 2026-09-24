package io.nekohasekai.sagernet.ui

import android.graphics.Color
import android.graphics.Rect
import android.view.Gravity
import android.view.LayoutInflater
import android.view.View
import android.widget.ImageView
import android.widget.LinearLayout
import android.widget.PopupWindow
import android.widget.ScrollView
import android.widget.TextView
import androidx.annotation.DrawableRes
import androidx.appcompat.content.res.AppCompatResources
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat
import io.nekohasekai.sagernet.R

data class UpwardAction(
    val id: Int,
    val title: CharSequence,
    @DrawableRes val icon: Int,
    val enabled: Boolean = true,
)

internal fun showUpwardActionMenu(
    anchor: View,
    actions: List<UpwardAction>,
    onAction: (Int) -> Unit,
) {
    val visibleActions = actions.filter { it.enabled }
    if (visibleActions.isEmpty()) return
    val context = anchor.context
    val density = context.resources.displayMetrics.density
    fun dp(value: Int) = (value * density).toInt()
    val content = LinearLayout(context).apply {
        orientation = LinearLayout.VERTICAL
        setPadding(dp(8), dp(6), dp(8), dp(6))
        background = AppCompatResources.getDrawable(context, R.drawable.bg_dashboard_group_menu_glass)
        elevation = dp(10).toFloat()
    }
    lateinit var window: PopupWindow
    visibleActions.forEach { action ->
        val row = LayoutInflater.from(context).inflate(R.layout.layout_group_action_outline, content, false)
        row.findViewById<ImageView>(R.id.group_action_icon).setImageResource(action.icon)
        row.findViewById<TextView>(R.id.group_action_title).text = action.title
        row.contentDescription = action.title
        row.setOnClickListener {
            window.dismiss()
            onAction(action.id)
        }
        content.addView(row)
    }
    val safe = Rect()
    anchor.getWindowVisibleDisplayFrame(safe)
    val root = anchor.rootView
    val rootLocation = IntArray(2)
    root.getLocationOnScreen(rootLocation)
    ViewCompat.getRootWindowInsets(root)?.getInsets(
        WindowInsetsCompat.Type.systemBars() or WindowInsetsCompat.Type.displayCutout()
    )?.let { insets ->
        safe.left = maxOf(safe.left, rootLocation[0] + insets.left)
        safe.top = maxOf(safe.top, rootLocation[1] + insets.top)
        safe.right = minOf(safe.right, rootLocation[0] + root.width - insets.right)
        safe.bottom = minOf(safe.bottom, rootLocation[1] + root.height - insets.bottom)
    }
    val location = IntArray(2)
    anchor.getLocationOnScreen(location)
    val margin = dp(8)
    val gap = dp(6)
    val availableWidth = (safe.width() - 2 * margin).coerceAtLeast(1)
    val availableHeight = (location[1] - gap - safe.top - margin).coerceAtLeast(1)
    content.measure(
        View.MeasureSpec.makeMeasureSpec(availableWidth, View.MeasureSpec.AT_MOST),
        View.MeasureSpec.makeMeasureSpec(0, View.MeasureSpec.UNSPECIFIED),
    )
    val popupWidth = content.measuredWidth.coerceIn(1, availableWidth)
    val popupHeight = content.measuredHeight.coerceIn(1, availableHeight)
    window = PopupWindow(ScrollView(context).apply { addView(content) }, popupWidth, popupHeight, true).apply {
        isOutsideTouchable = true
        elevation = dp(10).toFloat()
        setBackgroundDrawable(android.graphics.drawable.ColorDrawable(Color.TRANSPARENT))
    }
    val minX = safe.left + margin
    val x = (location[0] + anchor.width / 2 - popupWidth / 2)
        .coerceIn(minX, (safe.right - margin - popupWidth).coerceAtLeast(minX))
    val y = (location[1] - popupHeight - gap).coerceAtLeast(safe.top + margin)
    window.showAtLocation(root, Gravity.NO_GRAVITY, x, y)
}
