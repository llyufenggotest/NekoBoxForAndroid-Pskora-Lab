package io.nekohasekai.sagernet.ui

import android.content.Context
import android.content.res.ColorStateList
import android.text.TextUtils
import android.util.TypedValue
import android.view.Gravity
import android.view.View
import android.widget.ImageView
import android.widget.LinearLayout
import android.widget.TextView
import androidx.appcompat.widget.AppCompatImageButton
import androidx.appcompat.widget.SwitchCompat
import io.nekohasekai.sagernet.R
import io.nekohasekai.sagernet.database.RuleEntity

/** Compact presentation of real rules; unconfigured presets never enable routing. */
class StrategyPanel @JvmOverloads constructor(context: Context, attrs: android.util.AttributeSet? = null) : LinearLayout(context, attrs) {
    init { orientation = VERTICAL }
    private fun dp(value: Int) = (value * resources.displayMetrics.density).toInt()

    private fun themeColor(attribute: Int): Int {
        val values = context.obtainStyledAttributes(intArrayOf(attribute))
        return try { values.getColor(0, 0) } finally { values.recycle() }
    }

    private fun View.touchFeedback() {
        val value = TypedValue()
        if (context.theme.resolveAttribute(android.R.attr.selectableItemBackgroundBorderless, value, true)) {
            setBackgroundResource(value.resourceId)
        }
        isFocusable = true
    }

    private fun serviceIcon(id: String) = when (id) {
        "youtube" -> R.drawable.ic_strategy_service_play
        "tiktok" -> R.drawable.ic_strategy_service_music
        "telegram" -> R.drawable.ic_strategy_service_plane
        "netflix" -> R.drawable.ic_strategy_service_film
        "disney" -> R.drawable.ic_strategy_service_sparkle
        "meta" -> R.drawable.ic_strategy_service_people
        "spotify" -> R.drawable.ic_strategy_service_headphones
        "google" -> R.drawable.ic_strategy_service_search
        "ai" -> R.drawable.ic_strategy_service_sparkle
        else -> R.drawable.ic_strategy_service_globe
    }

    fun render(
        rules: List<RuleEntity>,
        edit: (SmartRoutePreset, RuleEntity?, Boolean) -> Unit,
        toggle: (RuleEntity, Boolean) -> Unit,
        delete: (RuleEntity) -> Unit,
    ) {
        removeAllViews()
        val secondary = themeColor(android.R.attr.textColorSecondary)
        val accent = themeColor(androidx.appcompat.R.attr.colorPrimary)
        smartRoutePresets().forEach { preset ->
            // Match complete criteria rather than editable names; retain existing callback semantics.
            val domains = parseSmartRouteTargets(preset.targets.joinToString("\n")).domains.toSet()
            val matches = rules.filter {
                it.canEditSimply() && it.ip.isBlank() &&
                    parseSmartRouteTargets(it.domains).domains.toSet() == domains
            }
            val rule = matches.firstOrNull()
            // Transparent top/bottom rules only; no rounded or selected-size container.
            val card = LinearLayout(context).apply {
                orientation = VERTICAL
                setBackgroundResource(R.drawable.bg_routing_row_separators)
            }
            addView(card, LayoutParams(LayoutParams.MATCH_PARENT, LayoutParams.WRAP_CONTENT).apply {
                bottomMargin = dp(4)
            })
            val row = LinearLayout(context).apply {
                orientation = HORIZONTAL
                gravity = Gravity.CENTER_VERTICAL
                minimumHeight = dp(72)
                setPadding(dp(12), dp(2), dp(4), dp(2))
            }
            card.addView(row)
            row.addView(ImageView(context).apply {
                setImageResource(serviceIcon(preset.id))
                imageTintList = ColorStateList.valueOf(secondary)
                importantForAccessibility = View.IMPORTANT_FOR_ACCESSIBILITY_NO
            }, LayoutParams(dp(24), dp(24)).apply { marginEnd = dp(10) })

            // Two compact text lines leave the switch and delete target independent on narrow screens.
            val details = LinearLayout(context).apply { orientation = VERTICAL }
            row.addView(details, LayoutParams(0, LayoutParams.WRAP_CONTENT, 1f))
            details.addView(TextView(context).apply {
                text = preset.title
                textSize = 14f
                maxLines = 1
                ellipsize = TextUtils.TruncateAt.END
                minimumHeight = dp(32)
                gravity = Gravity.CENTER_VERTICAL
                contentDescription = "${preset.title}，${preset.subtitle}，${if (rule == null) "配置目标" else "编辑目标"}"
                touchFeedback()
                setOnClickListener { edit(preset, rule, false) }
            }, LayoutParams(LayoutParams.MATCH_PARENT, LayoutParams.WRAP_CONTENT))
            details.addView(TextView(context).apply {
                val outbound = rule?.displayOutbound() ?: "未配置"
                text = outbound
                textSize = 12f
                setTextColor(accent)
                maxLines = 1
                ellipsize = TextUtils.TruncateAt.END
                minimumHeight = dp(32)
                gravity = Gravity.CENTER_VERTICAL
                compoundDrawablePadding = dp(3)
                setCompoundDrawablesRelativeWithIntrinsicBounds(0, 0, R.drawable.ic_strategy_chevron, 0)
                compoundDrawableTintList = ColorStateList.valueOf(accent)
                contentDescription = "${preset.title}，当前策略：$outbound，选择出口" +
                    if (matches.size > 1) "，另有同目标规则，请在规则列表逐条管理" else ""
                touchFeedback()
                setOnClickListener { edit(preset, rule, true) }
            }, LayoutParams(LayoutParams.MATCH_PARENT, LayoutParams.WRAP_CONTENT))
            row.addView(SwitchCompat(context).apply {
                contentDescription = if (rule == null) "${preset.title}策略未配置，默认关闭，请先选择出口" else "${preset.title}策略启用"
                isChecked = rule?.enabled == true
                isEnabled = rule != null
                minimumWidth = 0
                switchMinWidth = dp(40)
                setPadding(0, 0, 0, 0)
                setOnCheckedChangeListener { _, checked -> if (rule != null) toggle(rule, checked) }
            }, LayoutParams(dp(48), dp(48)).apply { marginStart = dp(4) })
            row.addView(AppCompatImageButton(context).apply {
                setImageResource(R.drawable.ic_strategy_delete_outline)
                imageTintList = ColorStateList.valueOf(secondary)
                scaleType = ImageView.ScaleType.CENTER_INSIDE
                setPadding(dp(10), dp(10), dp(10), dp(10))
                touchFeedback()
                isEnabled = rule != null
                alpha = if (rule == null) 0.32f else 1f
                contentDescription = if (rule == null) "${preset.title}未配置，无可删除规则" else "删除${preset.title}规则"
                setOnClickListener { if (rule != null) delete(rule) }
            }, LayoutParams(dp(44), dp(44)))
        }
    }
}
