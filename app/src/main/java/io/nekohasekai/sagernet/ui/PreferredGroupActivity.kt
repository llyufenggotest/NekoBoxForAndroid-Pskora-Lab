package io.nekohasekai.sagernet.ui

import android.content.Intent
import android.os.Bundle
import android.text.InputType
import android.widget.*
import android.view.ViewGroup
import androidx.recyclerview.widget.RecyclerView
import androidx.recyclerview.widget.LinearLayoutManager
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat
import androidx.lifecycle.lifecycleScope
import androidx.activity.OnBackPressedCallback
import kotlinx.coroutines.CancellationException
import io.nekohasekai.sagernet.GroupType
import com.google.android.material.dialog.MaterialAlertDialogBuilder
import io.nekohasekai.sagernet.database.*
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import moe.matsuri.nb4a.proxy.config.*

/** Managed profile editor. Source groups stay references and expand only at build time. */
class PreferredGroupActivity : ThemedActivity() {
    companion object { const val EXTRA_SELECT_MODE = "select_mode" }
    private lateinit var nameInput: EditText
    private lateinit var intervalInput: EditText
    private lateinit var toleranceInput: EditText
    private lateinit var modeInput: Spinner
    private lateinit var membersButton: Button
    private lateinit var sourcesButton: Button
    private lateinit var saveButton: Button
    private val members = linkedSetOf<Long>()
    private val sources = linkedSetOf<Long>()
    private var editing: ProxyEntity? = null
    private var groups = emptyList<ProxyGroup>()
    private var targetGroupId = 0L
    private var loaded = false

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val content = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            val pad = (20 * resources.displayMetrics.density).toInt()
            setPadding(pad, pad, pad, pad)
        }
        val scroll = ScrollView(this).apply { addView(content) }
        setContentView(scroll)
        ViewCompat.setOnApplyWindowInsetsListener(scroll) { v, insets ->
            val bars = insets.getInsets(WindowInsetsCompat.Type.systemBars() or WindowInsetsCompat.Type.ime())
            v.setPadding(bars.left, bars.top, bars.right, bars.bottom)
            insets
        }
        ViewCompat.requestApplyInsets(scroll)
        onBackPressedDispatcher.addCallback(this, object : OnBackPressedCallback(true) {
            override fun handleOnBackPressed() { confirmExit() }
        })
        content.addView(TextView(this).apply { text = "优选分组"; textSize = 24f })
        fun input(label: String, numeric: Boolean = false) = EditText(this).also {
            content.addView(TextView(this).apply { text = label })
            it.hint = label
            it.isSingleLine = true
            it.inputType = if (numeric) InputType.TYPE_CLASS_NUMBER else InputType.TYPE_CLASS_TEXT
            content.addView(it)
        }
        nameInput = input("名称")
        content.addView(TextView(this).apply { text = "优选模式" })
        modeInput = Spinner(this).apply {
            adapter = ArrayAdapter(this@PreferredGroupActivity, android.R.layout.simple_spinner_dropdown_item,
                listOf("最低延迟", "稳定备用"))
        }
        content.addView(modeInput)
        intervalInput = input("测速周期（秒）", true)
        toleranceInput = input("周期测速容差（毫秒；不限制拨号排序）", true)
        membersButton = Button(this).apply { setOnClickListener { chooseMembers() } }
        sourcesButton = Button(this).apply { setOnClickListener { chooseSources() } }
        content.addView(membersButton)
        content.addView(sourcesButton)
        content.addView(TextView(this).apply {
            text = "最低延迟：定期优选更快节点。稳定备用：当前健康时保持使用，拨号失败后尝试备用。" +
                "切换用于新连接，已建立连接不会迁移。来源分组更新后重新连接生效。" +
                "TunNet、外部插件和自定义配置不支持自动切换，请单独连接使用。"
        })
        saveButton = Button(this).apply { text = "保存"; setOnClickListener { save() } }
        content.addView(saveButton)
        content.addView(Button(this).apply { text = "取消"; setOnClickListener { confirmExit() } })
        enabled(false)
        lifecycleScope.launch {
            try {
                val id = intent.getLongExtra(ProfileSelectActivity.EXTRA_PROFILE_ID, 0L)
                withContext(Dispatchers.IO) {
                    PreferredGroupStore.migrateLegacy()
                    groups = SagerDatabase.groupDao.allGroups()
                    editing = if (id == 0L) null else requireNotNull(SagerDatabase.proxyDao.getById(id)) { "优选分组已删除" }
                    require(editing == null || editing?.configBean?.type == 2) { "此节点不是优选分组" }
                    targetGroupId = editing?.groupId ?: 0L
                }
                val bean = editing?.configBean
                members.addAll(savedInstanceState?.getLongArray("members")?.toList()
                    ?: bean?.preferredMemberIds ?: intent.getLongArrayExtra(EXTRA_PREFERRED_MEMBER_IDS)?.toList().orEmpty())
                sources.addAll(savedInstanceState?.getLongArray("sources")?.toList()
                    ?: bean?.preferredSourceGroupIds ?: intent.getLongArrayExtra(EXTRA_PREFERRED_SOURCE_GROUP_IDS)?.toList().orEmpty())
                nameInput.setText(savedInstanceState?.getString("name") ?: bean?.name ?: intent.getStringExtra(EXTRA_PREFERRED_NAME).orEmpty())
                intervalInput.setText(savedInstanceState?.getString("interval") ?: (bean?.preferredIntervalSeconds
                    ?: intent.getIntExtra(EXTRA_PREFERRED_INTERVAL_SECONDS, 300)).toString())
                toleranceInput.setText(savedInstanceState?.getString("tolerance") ?: (bean?.preferredMinDelayMilliseconds
                    ?: intent.getIntExtra(EXTRA_PREFERRED_MIN_DELAY_MILLISECONDS, 0)).toString())
                val mode = savedInstanceState?.getString("mode") ?: bean?.preferredMode
                    ?: intent.getStringExtra(EXTRA_PREFERRED_MODE) ?: PREFERRED_MODE_LATENCY
                require(mode == PREFERRED_MODE_LATENCY || mode == PREFERRED_MODE_STABLE) { "未知优选模式：$mode" }
                modeInput.setSelection(if (mode == PREFERRED_MODE_STABLE) 1 else 0)
                loaded = true
                content.addView(TextView(this@PreferredGroupActivity).apply {
                    text = "保存为首页独立分组，与订阅分组平级；原节点保留在来源分组。"
                })
                updateCounts()
                enabled(true)
            } catch (e: CancellationException) { throw e }
            catch (e: Exception) { errorDialog(e.message ?: "加载失败") }
        }
    }

    override fun onSaveInstanceState(outState: Bundle) {
        super.onSaveInstanceState(outState)
        if (!loaded) return
        outState.putLongArray("members", members.toLongArray())
        outState.putLongArray("sources", sources.toLongArray())
        outState.putString("name", nameInput.text.toString())
        outState.putString("interval", intervalInput.text.toString())
        outState.putString("tolerance", toleranceInput.text.toString())
        outState.putString("mode", selectedMode())
    }

    private fun selectedMode() = if (modeInput.selectedItemPosition == 1) PREFERRED_MODE_STABLE else PREFERRED_MODE_LATENCY

    private fun enabled(value: Boolean) {
        nameInput.isEnabled = value
        intervalInput.isEnabled = value
        toleranceInput.isEnabled = value
        modeInput.isEnabled = value
        membersButton.isEnabled = value; sourcesButton.isEnabled = value; saveButton.isEnabled = value
    }
    private fun updateCounts() {
        membersButton.text = "选择节点（${members.size}）"
        sourcesButton.text = "动态引用整组（${sources.size}）"
    }
    private fun confirmExit() {
        if (!loaded) { finish(); return }
        MaterialAlertDialogBuilder(this).setTitle("放弃未保存的修改？")
            .setPositiveButton("放弃") { _, _ -> finish() }
            .setNegativeButton("继续编辑", null).show()
    }
    private fun errorDialog(message: String) {
        if (isFinishing || isDestroyed) return
        MaterialAlertDialogBuilder(this).setTitle("无法使用优选分组").setMessage(message)
            .setPositiveButton(android.R.string.ok, null).show()
    }
    private fun chooseMembers() {
        val chosen = members.toMutableSet()
        val box = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL }
        val back = Button(this).apply { text = "返回分组"; isEnabled = false }
        val list = RecyclerView(this).apply { layoutManager = LinearLayoutManager(this@PreferredGroupActivity) }
        box.addView(back)
        box.addView(list, LinearLayout.LayoutParams(-1, (420 * resources.displayMetrics.density).toInt()))
        val dialog = MaterialAlertDialogBuilder(this).setTitle("按分组选择节点")
            .setView(box).setPositiveButton("确定") { _, _ -> members.clear(); members.addAll(chosen); updateCounts() }
            .setNegativeButton("取消", null).create()
        var generation = 0
        fun showNodes(group: ProxyGroup?) {
            val ticket = ++generation
            back.isEnabled = true
            list.adapter = null
            lifecycleScope.launch {
                try {
                    val nodes = withContext(Dispatchers.IO) {
                        if (group == null) chosen.mapNotNull { SagerDatabase.proxyDao.getById(it) }
                        else SagerDatabase.proxyDao.getByGroup(group.id)
                    }
                    if (!dialog.isShowing || ticket != generation) return@launch
                    list.adapter = object : RecyclerView.Adapter<ChoiceHolder>() {
                        override fun getItemCount() = nodes.size
                        override fun onCreateViewHolder(parent: ViewGroup, type: Int) = ChoiceHolder(CheckBox(parent.context))
                        override fun onBindViewHolder(holder: ChoiceHolder, position: Int) {
                            val node = nodes[position]
                            val check = holder.label as CheckBox
                            check.setOnCheckedChangeListener(null)
                            check.text = node.displayName() + if (node.configBean?.type == 2) "（不能嵌套优选）" else ""
                            check.isChecked = node.id in chosen
                            check.isEnabled = node.configBean?.type != 2 || check.isChecked
                            check.setOnCheckedChangeListener { _, checked ->
                                if (checked) chosen.add(node.id) else chosen.remove(node.id)
                            }
                        }
                    }
                } catch (e: CancellationException) { throw e }
                catch (e: Exception) { errorDialog(e.message ?: "读取分组失败") }
            }
        }
        fun showGroups() {
            generation++
            back.isEnabled = false
            val available = groups.filter { it.type != GroupType.PREFERRED }
            list.adapter = object : RecyclerView.Adapter<ChoiceHolder>() {
                override fun getItemCount() = available.size + 2
                override fun onCreateViewHolder(parent: ViewGroup, type: Int) = ChoiceHolder(Button(parent.context))
                override fun onBindViewHolder(holder: ChoiceHolder, position: Int) {
                    holder.label.text = when (position) {
                        0 -> "查看已选节点（${chosen.size}）"
                        1 -> "清空已选（包括失效引用）"
                        else -> available[position - 2].displayName() + "  ›"
                    }
                    holder.label.setOnClickListener {
                        when (position) {
                            0 -> showNodes(null)
                            1 -> { chosen.clear(); showGroups() }
                            else -> showNodes(available[position - 2])
                        }
                    }
                }
            }
        }
        back.setOnClickListener { showGroups() }
        dialog.setOnDismissListener { generation++ }
        dialog.show()
        showGroups()
    }

    private class ChoiceHolder(val label: TextView) : RecyclerView.ViewHolder(label) {
        init { label.layoutParams = RecyclerView.LayoutParams(-1, -2) }
    }
    private fun chooseSources() {
        val ids = (groups.filter { it.type != GroupType.PREFERRED }.map { it.id } + sources).distinct()
        val chosen = sources.toMutableSet()
        val labels = ids.map { id ->
            (groups.find { it.id == id }?.displayName() ?: "已删除分组 #$id") +
                if (id == targetGroupId) "（不可引用自身所在组）" else ""
        }.toTypedArray()
        MaterialAlertDialogBuilder(this).setTitle("整组动态引用（保存时校验全部节点）")
            .setMultiChoiceItems(labels, ids.map { it in chosen }.toBooleanArray()) { dialog, which, checked ->
                val id = ids[which]
                if (checked && (id == targetGroupId || groups.none { it.id == id && it.type != GroupType.PREFERRED })) {
                    (dialog as androidx.appcompat.app.AlertDialog).listView.setItemChecked(which, false)
                } else if (checked) chosen.add(id) else chosen.remove(id)
            }.setPositiveButton(android.R.string.ok) { _, _ -> sources.clear(); sources.addAll(chosen); updateCounts() }
            .setNegativeButton(android.R.string.cancel, null).show()
    }
    private fun save() {
        val bean = (editing?.configBean?.clone() ?: ConfigBean()).apply {
            initializeDefaultValues()
            type = 2
            name = nameInput.text.toString().trim()
            preferredMode = selectedMode()
            preferredMemberIds = members.toList()
            // Explicit re-selection restores a previously excluded source ID.
            preferredExcludedMemberIds = preferredExcludedMemberIds.orEmpty().filterNot { it in members }
            preferredSourceGroupIds = sources.toList()
            preferredIntervalSeconds = intervalInput.text.toString().toIntOrNull() ?: 0
            preferredMinDelayMilliseconds = toleranceInput.text.toString().toIntOrNull() ?: -1
        }
        enabled(false)
        lifecycleScope.launch {
            try {
                val saved = withContext(Dispatchers.IO) {
                    require(bean.name.isNotBlank()) { "请输入分组名称" }
                    PreferredGroupStore.save(bean, editing).also { persisted ->
                        DataStore.selectedGroup = persisted.groupId
                        GroupManager.iterator { groupUpdated(persisted.groupId) }
                        ProfileManager.postUpdate(persisted)
                    }
                }.let { persisted ->
                    withContext(Dispatchers.IO) {
                        requireNotNull(SagerDatabase.proxyDao.getById(persisted.id)) { "保存后未找到优选分组" }.also {
                            require(it.configBean?.preferredSpec() == bean.preferredSpec() && it.configBean?.name == bean.name && it.configBean?.type == 2) { "保存校验失败，请重新打开" }
                        }
                    }
                }
                // Both entry modes return the persisted Long id; select_mode callers can consume directly.
                setResult(RESULT_OK, Intent().putExtra(ProfileSelectActivity.EXTRA_PROFILE_ID, saved.id))
                finish()
            } catch (e: CancellationException) { throw e }
            catch (e: Exception) {
                enabled(true)
                errorDialog(e.message ?: "保存失败")
            }
        }
    }
}
