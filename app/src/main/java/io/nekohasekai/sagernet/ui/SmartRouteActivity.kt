package io.nekohasekai.sagernet.ui

import android.content.Intent
import android.os.Bundle
import android.text.InputType
import android.view.ViewGroup
import android.widget.*
import androidx.activity.result.contract.ActivityResultContracts
import androidx.lifecycle.lifecycleScope
import com.google.android.material.dialog.MaterialAlertDialogBuilder
import io.nekohasekai.sagernet.R
import io.nekohasekai.sagernet.database.ProfileManager
import io.nekohasekai.sagernet.database.ProxyEntity
import io.nekohasekai.sagernet.database.RuleEntity
import io.nekohasekai.sagernet.database.SagerDatabase
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/** Only losslessly representable rules are edited here. Advanced rules retain their editor. */
fun RuleEntity.canEditSimply(): Boolean =
    listOf(config, port, sourcePort, network, source, protocol, ruleset).all { it.isEmpty() } &&
        packages.isEmpty() && (domains.isBlank() xor ip.isBlank()) &&
        !domains.lineSequence().any { it.isNotBlank() && !it.startsWith("domain:") } &&
        parseSmartRouteTargets(if (domains.isNotBlank()) domains else ip).simpleModeError() == null

class SmartRouteActivity : ThemedActivity(R.layout.layout_empty) {
    companion object {
        const val EXTRA_PRESET = "preset"
        const val EXTRA_SELECT_OUTBOUND = "select_outbound"
        // PreferredGroupActivity returns ProfileSelectActivity.EXTRA_PROFILE_ID ("id").
        const val PREFERRED_GROUP_ACTIVITY = "io.nekohasekai.sagernet.ui.PreferredGroupActivity"
    }
    private lateinit var nameInput: EditText
    private lateinit var targetsInput: EditText
    private lateinit var outboundButton: Button
    private lateinit var saveButton: Button
    private lateinit var enabledInput: androidx.appcompat.widget.SwitchCompat
    private var initialEnabled = false
    private var original: RuleEntity? = null
    private var outbound = 0L
    private var loaded = false
    private var saving = false
    private var initialName = ""
    private var initialTargets = ""
    private var initialOutbound = 0L

    private val selectProfile = registerForActivityResult(ActivityResultContracts.StartActivityForResult()) { result ->
        if (result.resultCode == RESULT_OK) {
            val selected = result.data?.getLongExtra(ProfileSelectActivity.EXTRA_PROFILE_ID, 0L) ?: 0L
            if (selected > 0) { outbound = selected; refreshOutbound() }
            else message("没有返回有效节点，请重新选择")
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val holder = findViewById<ViewGroup>(R.id.fragment_holder)
        val scroll = ScrollView(this)
        val body = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            val padding = (20 * resources.displayMetrics.density).toInt()
            setPadding(padding, padding, padding, padding)
        }
        holder.addView(scroll, ViewGroup.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT))
        androidx.core.view.ViewCompat.setOnApplyWindowInsetsListener(holder) { view, insets ->
            val bars = insets.getInsets(androidx.core.view.WindowInsetsCompat.Type.systemBars() or androidx.core.view.WindowInsetsCompat.Type.ime())
            view.setPadding(bars.left, bars.top, bars.right, bars.bottom)
            insets
        }
        androidx.core.view.ViewCompat.requestApplyInsets(holder)
        scroll.addView(body)
        fun label(text: String) = body.addView(TextView(this).apply { this.text = text; textSize = 16f })
        fun button(text: String, action: () -> Unit): Button = Button(this).apply {
            this.text = text
            body.addView(this)
            setOnClickListener { action() }
        }
        label("应用策略")
        label("填写目标 → 选择节点 → 保存。规则按列表从上到下匹配；长按拖动可调整优先级。")
        nameInput = EditText(this).apply { hint = "规则名称"; isSingleLine = true; body.addView(this) }
        label("域名包含子域名；IP 支持 IPv4、IPv6 和 CIDR。每行一个，同一条规则不要混合域名和 IP。")
        targetsInput = EditText(this).apply {
            hint = "example.com\n或 1.1.1.1 / 10.0.0.0/8"
            inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_FLAG_MULTI_LINE or InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS
            minLines = 5
            body.addView(this)
        }
        outboundButton = button("选择出口") { chooseOutbound() }
        label("选择节点、链式节点或优选组作为整条策略的出口。优选组可包含多个候选节点。")
        enabledInput = androidx.appcompat.widget.SwitchCompat(this).apply {
            text = "启用此策略"
            body.addView(this)
        }
        label("新策略默认关闭；保存后也可在策略卡片上开启。")
        saveButton = button("保存规则") { save() }
        button("高级编辑（原有全部选项）") {
            if (loaded && !saving) {
                if (dirty()) MaterialAlertDialogBuilder(this).setMessage("放弃未保存的简洁编辑并打开高级编辑？")
                    .setPositiveButton("继续") { _, _ -> openAdvanced() }
                    .setNegativeButton(android.R.string.cancel, null).show()
                else openAdvanced()
            }
        }
        button("取消") { onBackPressed() }
        saveButton.isEnabled = false
        lifecycleScope.launch {
            val id = intent.getLongExtra(RouteSettingsActivity.EXTRA_ROUTE_ID, 0L)
            val entity = withContext(Dispatchers.IO) { if (id == 0L) null else SagerDatabase.rulesDao.getById(id) }
            if (id != 0L && entity == null) { message("规则已被删除"); finish(); return@launch }
            original = entity
            if (entity != null && !entity.canEditSimply()) { openAdvanced(); return@launch }
            val preset = smartRoutePresets().find { it.id == intent.getStringExtra(EXTRA_PRESET) }
            initialName = entity?.name ?: preset?.title.orEmpty()
            initialTargets = entity?.let { if (it.domains.isNotBlank()) it.domains else it.ip }
                ?: preset?.targets?.joinToString("\n").orEmpty()
            initialOutbound = entity?.outbound ?: 0L
            initialEnabled = entity?.enabled ?: false
            enabledInput.isChecked = savedInstanceState?.getBoolean("enabled") ?: initialEnabled
            nameInput.setText(savedInstanceState?.getString("name") ?: initialName)
            targetsInput.setText(savedInstanceState?.getString("targets") ?: initialTargets)
            outbound = savedInstanceState?.getLong("outbound") ?: initialOutbound
            loaded = true
            saveButton.isEnabled = true
            refreshOutbound()
            if (savedInstanceState == null && intent.getBooleanExtra(EXTRA_SELECT_OUTBOUND, false)) chooseOutbound()
        }
    }

    private fun chooseOutbound() {
        if (!loaded || saving) return
        MaterialAlertDialogBuilder(this).setTitle("选择策略出口")
            .setItems(arrayOf("当前代理节点", "直连", "拦截", "选择已有节点 / 链式节点 / 优选组", "新建优选组（多选节点）", "绕过此策略（继续匹配后续规则）")) { _, which ->
                when (which) {
                    3 -> selectProfile.launch(Intent(this, ProfileSelectActivity::class.java))
                    4 -> {
                        val create = Intent().setClassName(this, PREFERRED_GROUP_ACTIVITY)
                            .putExtra("select_mode", true)
                        if (create.resolveActivity(packageManager) != null) selectProfile.launch(create)
                        else message("优选组编辑器尚未安装；可先选择已有节点或链式节点")
                    }
                    5 -> {
                        enabledInput.isChecked = false
                        message("保存后停用此策略，流量继续匹配后续规则；这与直连不同")
                    }
                    else -> { outbound = -which.toLong(); refreshOutbound() }
                }
            }.show()
    }

    private fun refreshOutbound() {
        val selected = outbound
        lifecycleScope.launch {
            val profile = withContext(Dispatchers.IO) { if (selected > 0) ProfileManager.getProfile(selected) else null }
            if (selected != outbound) return@launch
            outboundButton.text = when (selected) {
                0L -> "出口：当前代理节点"
                -1L -> "出口：直连"
                -2L -> "出口：拦截"
                else -> "出口：" + (profile?.let {
                    (if (it.type == ProxyEntity.TYPE_CHAIN) "[链式] " else "") + it.displayName()
                } ?: "节点已删除，请重新选择")
            }
        }
    }

    private fun save() {
        if (!loaded || saving) return
        val parsed = parseSmartRouteTargets(targetsInput.text.toString())
        parsed.simpleModeError()?.let { targetsInput.error = it; return }
        val name = nameInput.text.toString().trim()
        val targetText = targetsInput.text.toString()
        val selected = outbound
        val enabled = enabledInput.isChecked
        val nameChanged = name != initialName
        val targetChanged = targetText != initialTargets
        saving = true
        saveButton.isEnabled = false
        lifecycleScope.launch {
            try {
                withContext(Dispatchers.IO) {
                    check(selected <= 0 || ProfileManager.getProfile(selected) != null) { "节点已删除，请重新选择" }
                    val old = original
                    val rule = if (old == null) RuleEntity(enabled = false) else {
                        val current = SagerDatabase.rulesDao.getById(old.id)
                        check(current == old) { "规则已被修改或删除，请返回列表重新打开" }
                        current!!.copy()
                    }
                    if (old == null || nameChanged) rule.name = name
                    rule.outbound = selected
                    rule.enabled = enabled
                    if (old == null || targetChanged) {
                        rule.domains = parsed.domains.joinToString("\n")
                        rule.ip = parsed.ips.joinToString("\n")
                    }
                    if (old == null) ProfileManager.createRule(rule) else ProfileManager.updateRule(rule)
                    check(SagerDatabase.rulesDao.getById(rule.id) == rule) { "规则保存校验失败" }
                }
                finish()
            } catch (e: Exception) {
                if (e is kotlinx.coroutines.CancellationException) throw e
                message(e.message ?: "保存失败")
                saving = false
                saveButton.isEnabled = true
            }
        }
    }

    private fun openAdvanced() {
        startActivity(Intent(this, RouteSettingsActivity::class.java).apply {
            putExtra(RouteSettingsActivity.EXTRA_ROUTE_ID, intent.getLongExtra(RouteSettingsActivity.EXTRA_ROUTE_ID, 0L))
        })
        finish()
    }
    private fun message(text: String) { Toast.makeText(this, text, Toast.LENGTH_LONG).show() }
    private fun dirty() = loaded && (nameInput.text.toString() != initialName || targetsInput.text.toString() != initialTargets || outbound != initialOutbound || enabledInput.isChecked != initialEnabled)
    override fun onBackPressed() {
        if (saving) return
        if (!dirty()) { super.onBackPressed(); return }
        MaterialAlertDialogBuilder(this).setMessage("放弃未保存的修改？")
            .setPositiveButton("放弃") { _, _ -> finish() }
            .setNegativeButton(android.R.string.cancel, null).show()
    }
    override fun onSaveInstanceState(outState: Bundle) {
        if (loaded) {
            outState.putString("name", nameInput.text.toString())
            outState.putString("targets", targetsInput.text.toString())
            outState.putLong("outbound", outbound)
            outState.putBoolean("enabled", enabledInput.isChecked)
        }
        super.onSaveInstanceState(outState)
    }
}
