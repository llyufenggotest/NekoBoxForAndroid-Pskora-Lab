package io.nekohasekai.sagernet.ui

import android.os.Bundle
import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import android.widget.LinearLayout
import android.widget.TextView
import android.widget.Toast
import androidx.core.view.isVisible
import android.content.Intent
import io.nekohasekai.sagernet.fmt.toUniversalLink
import io.nekohasekai.sagernet.GroupType
import io.nekohasekai.sagernet.ktx.*
import androidx.fragment.app.Fragment
import androidx.lifecycle.lifecycleScope
import androidx.recyclerview.widget.DiffUtil
import androidx.recyclerview.widget.RecyclerView
import androidx.appcompat.widget.PopupMenu
import io.nekohasekai.sagernet.R
import io.nekohasekai.sagernet.SagerNet
import io.nekohasekai.sagernet.aidl.TrafficData
import io.nekohasekai.sagernet.bg.BaseService
import io.nekohasekai.sagernet.database.*
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.coroutines.CancellationException
import moe.matsuri.nb4a.Protocols.getProtocolColor

/** Source references, not copies. A member click selects the automatic container, not that leaf. */
class PreferredGroupFragment : Fragment(), ProfileManager.Listener, GroupManager.Listener {
    companion object {
        fun newInstance(id: Long, select: Boolean) = PreferredGroupFragment().apply {
            arguments = Bundle().apply { putLong("group", id); putBoolean("select", select) }
        }
    }
    private val groupId get() = requireArguments().getLong("group")
    private var owner: ProxyEntity? = null
    private var rows = emptyList<Pair<ProxyEntity?, String>>()
    private var list: RecyclerView? = null
    private var status: TextView? = null
    private var generation = 0
    private var activeMemberIds = emptySet<Long>()
    private fun rowId(row: Pair<ProxyEntity?, String>): Long = row.first?.id
        ?: (Long.MIN_VALUE + row.second.hashCode().toLong())
    private fun sortedRows(source: List<Pair<ProxyEntity?, String>>) = preferredMembersByLatency(
        source,
        id = { rowId(it) },
        persistedStatus = { it.first?.status ?: 0 },
        persistedPing = { it.first?.ping ?: 0 },
    )
    private fun submitRows(source: List<Pair<ProxyEntity?, String>>) {
        val next = sortedRows(source)
        val previous = rows
        if (next == previous) {
            if (next.isNotEmpty()) cards.notifyItemRangeChanged(0, next.size, Unit)
            return
        }
        val diff = DiffUtil.calculateDiff(object : DiffUtil.Callback() {
            override fun getOldListSize() = previous.size
            override fun getNewListSize() = next.size
            override fun areItemsTheSame(oldPosition: Int, newPosition: Int) =
                rowId(previous[oldPosition]) == rowId(next[newPosition])
            override fun areContentsTheSame(oldPosition: Int, newPosition: Int) =
                previous[oldPosition] == next[newPosition]
        }, false)
        rows = next
        diff.dispatchUpdatesTo(cards)
    }
    private val cards = object : RecyclerView.Adapter<Card>() {
        init { setHasStableIds(true) }
        override fun getItemCount() = rows.size
        override fun getItemId(position: Int): Long = rows[position].first?.id ?: rowId(rows[position])
        override fun onCreateViewHolder(parent: ViewGroup, viewType: Int) = Card(
            LayoutInflater.from(parent.context).inflate(R.layout.layout_profile, parent, false)
        )
        override fun onBindViewHolder(holder: Card, position: Int) {
            val (node, source) = rows[position]
            holder.name.text = node?.displayName() ?: source
            holder.type.text = node?.displayType().orEmpty()
            holder.sourceGroup.text = "引用分组 · $source"
            holder.sourceGroup.visibility = if (node == null) View.GONE else View.VISIBLE
            holder.type.setTextColor(node?.let { requireContext().getProtocolColor(it.type) }
                ?: requireContext().getColorAttr(android.R.attr.textColorSecondary))
            // status/ping are the persisted test of this exact source ID. Never use owner.ping
            // or invoke urlTest on opening this page. Errors stay compact, without endpoint detail.
            holder.health.text = node?.let { preferredTestResults.numericLabel(it.id, it.status, it.ping) }.orEmpty()
            holder.health.contentDescription = holder.health.text
            holder.health.setTextColor(when (node?.let { preferredTestResults.tone(it.id, it.status) }) {
                PreferredHealthTone.HEALTHY -> requireContext().getColour(R.color.material_green_500)
                PreferredHealthTone.UNHEALTHY -> requireContext().getColour(R.color.material_red_500)
                else -> requireContext().getColorAttr(android.R.attr.textColorSecondary)
            })
            val ownerId = owner?.id ?: 0L
            val ownerActions = nodeDiagnosticActions(
                DataStore.serviceState,
                ownerId,
                DataStore.selectedProxy,
                DataStore.currentProfile,
            )
            val isRuntimeMember = node?.id in activeMemberIds
            val qualityVisible = ownerActions.qualityVisible && isRuntimeMember
            val speedVisible = ownerActions.speedVisible && isRuntimeMember
            holder.leaf.visibility = if (qualityVisible) View.VISIBLE else View.GONE
            (holder.leaf as? android.widget.ImageButton)?.setColorFilter(
                requireContext().getColour(when ((parentFragment as? ConfigurationFragment)?.qualityTier(ownerId)) {
                    "green" -> R.color.material_green_500
                    "yellow" -> R.color.material_amber_500
                    "red" -> R.color.material_red_500
                    else -> R.color.profile_card_icon
                })
            )
            holder.speed.visibility = if (speedVisible) View.VISIBLE else View.GONE
            val parent = parentFragment as? ConfigurationFragment
            val rates = if (isRuntimeMember) parent?.nodeRatesFor(ownerId) else null
            val speedText = if (isRuntimeMember) parent?.speedDisplayFor(ownerId) else null
            holder.uploadSpeed.text = speedText?.first ?: rates?.first?.let {
                "↑" + android.text.format.Formatter.formatFileSize(requireContext(), it) + "/s"
            }.orEmpty()
            holder.downloadSpeed.text = speedText?.second ?: rates?.second?.let {
                "↓" + android.text.format.Formatter.formatFileSize(requireContext(), it) + "/s"
            }.orEmpty()
            holder.uploadSpeed.visibility = if (speedVisible && holder.uploadSpeed.text.isNotEmpty()) View.VISIBLE else View.GONE
            holder.downloadSpeed.visibility = if (speedVisible && holder.downloadSpeed.text.isNotEmpty()) View.VISIBLE else View.GONE
            holder.lightning.visibility = if (node != null) View.VISIBLE else View.GONE
            holder.lightning.isEnabled = ownerActions.latencyEnabled
            holder.leaf.setOnClickListener {
                (parentFragment as? ConfigurationFragment)?.showIPQualityForProfile(ownerId)
            }
            holder.speed.setOnClickListener {
                (parentFragment as? ConfigurationFragment)?.startSpeedTestForProfile(ownerId, 1)
            }
            holder.speed.setOnLongClickListener {
                (parentFragment as? ConfigurationFragment)?.startSpeedTestForProfile(ownerId, 8)
                true
            }
            holder.lightning.setOnClickListener { node?.let { testSingleMember(it) } }
            listOf(R.id.edit, R.id.share, R.id.remove).forEach { id ->
                holder.itemView.findViewById<View>(id).apply {
                    visibility = if (node != null && !requireArguments().getBoolean("select")) View.VISIBLE else View.GONE
                    contentDescription = when (id) { R.id.edit -> "编辑原节点（影响所有引用）"; R.id.share -> "分享原节点"; else -> "移除优选引用（保留原节点）" }
                    setOnClickListener { node?.let { sourceAction(it, id) } }
                }
            }
            holder.bindLayoutMode(
                doubleColumn = isDoubleColumn(),
                showActions = node != null && !requireArguments().getBoolean("select"),
            )
            holder.menu.setOnClickListener { anchor ->
                node?.let { showDoubleColumnMenu(anchor, it) }
            }
            holder.itemView.contentDescription = if (node == null) source else
                "${node.displayName()}，${holder.type.text}，${holder.health.text}；使用整组自动优选"
            holder.itemView.setOnClickListener { connect() }
        }
    }
    private class Card(view: View) : RecyclerView.ViewHolder(view) {
        val name: TextView = view.findViewById(R.id.profile_name)
        val sourceGroup: TextView = view.findViewById(R.id.profile_source_group)
        val type: TextView = view.findViewById(R.id.profile_type)
        val health: TextView = view.findViewById(R.id.profile_status)
        val uploadSpeed: TextView = view.findViewById(R.id.profile_upload_speed)
        val downloadSpeed: TextView = view.findViewById(R.id.profile_download_speed)
        val leaf: View = view.findViewById(R.id.profile_leaf)
        val speed: View = view.findViewById(R.id.profile_speedometer)
        val lightning: View = view.findViewById(R.id.profile_lightning)
        val menu: View = view.findViewById(R.id.double_column_menu)
        private val edit: View = view.findViewById(R.id.edit)
        private val share: View = view.findViewById(R.id.share)
        private val remove: View = view.findViewById(R.id.remove)
        fun bindLayoutMode(doubleColumn: Boolean, showActions: Boolean) {
            val density = itemView.resources.displayMetrics.density
            (itemView.layoutParams as? ViewGroup.MarginLayoutParams)?.let { params ->
                if (doubleColumn) {
                    params.marginStart = (2 * density).toInt()
                    params.marginEnd = (2 * density).toInt()
                    params.topMargin = (2 * density).toInt()
                    params.bottomMargin = (2 * density).toInt()
                } else {
                    val inset = itemView.resources.getDimensionPixelSize(R.dimen.lab_content_inset)
                    params.marginStart = inset
                    params.marginEnd = inset
                    params.topMargin = (4 * density).toInt()
                    params.bottomMargin = (4 * density).toInt()
                }
                itemView.layoutParams = params
            }
            leaf.visibility = if (!doubleColumn && leaf.visibility == View.VISIBLE) View.VISIBLE else View.GONE
            speed.visibility = if (!doubleColumn && speed.visibility == View.VISIBLE) View.VISIBLE else View.GONE
            edit.visibility = if (!doubleColumn && showActions) View.VISIBLE else View.GONE
            share.visibility = if (!doubleColumn && showActions) View.VISIBLE else View.GONE
            remove.visibility = if (!doubleColumn && showActions) View.VISIBLE else View.GONE
            menu.visibility = if (doubleColumn && showActions) View.VISIBLE else View.GONE
        }
        init {
            // Same compact separators/typography as ordinary rows. The source owns editing;
            // container edit/delete remain in the existing upward group-tab menu.
            menu.visibility = View.GONE
            view.findViewById<View>(R.id.selected_indicator).visibility = View.GONE
            (view.findViewById<TextView>(R.id.profile_address).parent as View).visibility = View.GONE
            type.maxLines = 1
            type.ellipsize = android.text.TextUtils.TruncateAt.END
            view.findViewById<View>(R.id.content_lin).isFocusable = false
        }
    }

    fun refreshDiagnosticCards() {
        if (cards.itemCount > 0) cards.notifyItemRangeChanged(0, cards.itemCount, Unit)
    }

    private fun testSingleMember(profile: ProxyEntity) {
        if (DataStore.serviceState != BaseService.State.Stopped &&
            DataStore.serviceState != BaseService.State.Idle) return
        val ticket = preferredTestResults.begin(profile.id, "URLTest")
        viewLifecycleOwner.lifecycleScope.launch(Dispatchers.IO) {
            try {
                profile.ping = io.nekohasekai.sagernet.bg.proto.UrlTest().doTest(profile)
                profile.status = 1
                profile.error = null
            } catch (e: Exception) {
                profile.status = 3
                profile.error = e.readableMessage
            }
            preferredTestResults.complete(profile.id, ticket, profile.status, profile.ping)
            ProfileManager.updateProfile(profile)
            withContext(Dispatchers.Main) { refreshDiagnosticCards() }
        }
    }

    private fun isDoubleColumn() = DataStore.groupLayoutMode == 1

    private fun applyLayoutManager(recyclerView: RecyclerView) {
        recyclerView.layoutManager = if (isDoubleColumn()) {
            FixedGridLayoutManager(recyclerView, 2)
        } else {
            FixedLinearLayoutManager(recyclerView)
        }
    }

    fun switchLayoutMode() {
        val recyclerView = list ?: return
        applyLayoutManager(recyclerView)
        cards.notifyItemRangeChanged(0, cards.itemCount)
    }

    private fun dp(value: Int) = (value * resources.displayMetrics.density).toInt()

    override fun onCreateView(inflater: LayoutInflater, container: ViewGroup?, state: Bundle?): View =
        LinearLayout(requireContext()).apply {
            orientation = LinearLayout.VERTICAL
            // Only failures/empty state need a header. Do not repeat the selected tab/group name.
            status = TextView(context).also {
                it.visibility = View.GONE
                it.textSize = 14f
                val pad = (12 * resources.displayMetrics.density).toInt()
                it.setPadding(pad, pad, pad, pad)
                addView(it)
            }
            list = RecyclerView(context).also {
                applyLayoutManager(it)
                val horizontalPadding = dp(4)
                val topPadding = dp(4)
                val bottomPadding = dp(156)
                it.setPadding(horizontalPadding, topPadding, horizontalPadding, bottomPadding)
                it.clipToPadding = false
                it.clipChildren = false
                it.adapter = cards
                addView(it, LinearLayout.LayoutParams(-1, 0, 1f))
            }
        }

    override fun onViewCreated(view: View, state: Bundle?) {
        ProfileManager.addListener(this)
        GroupManager.addListener(this)
    }
    private var selectionJob: kotlinx.coroutines.Job? = null
    override fun onResume() {
        super.onResume(); refresh()
        selectionJob = viewLifecycleOwner.lifecycleScope.launch {
            while (true) {
                val profileId = owner?.id
                val active = DataStore.serviceState.connected && DataStore.currentProfile == profileId
                if (profileId != null && active && rows.isNotEmpty()) {
                    val service = (activity as? MainActivity)?.connection?.service
                    var snapshot: org.json.JSONObject? = null
                    val label = withContext(Dispatchers.IO) {
                        preferredRuntimeLabel(service, profileId) { snapshot = it }
                    }
                    if (owner?.id == profileId && DataStore.serviceState.connected && DataStore.currentProfile == profileId) {
                        activeMemberIds = setOfNotNull(
                            snapshot?.optLong("tcpId")?.takeIf { it > 0L },
                            snapshot?.optLong("udpId")?.takeIf { it > 0L },
                        )
                        preferredTestResults.replaceAutomatic(snapshot?.optString("session").orEmpty(),
                            snapshot?.let { readPreferredAutomatic(it) }.orEmpty())
                        status?.text = label
                        status?.visibility = View.VISIBLE
                    }
                } else if (owner != null && rows.isNotEmpty()) {
                    activeMemberIds = emptySet()
                    preferredTestResults.replaceAutomatic("", emptyMap())
                    status?.text = ""
                    status?.visibility = View.GONE
                }
                // Manual/automatic results reorder by exact source ID; DiffUtil keeps holders stable.
                submitRows(rows)
                kotlinx.coroutines.delay(1000)
            }
        }
    }
    override fun onPause() { selectionJob?.cancel(); selectionJob = null; super.onPause() }
    override fun onDestroyView() {
        generation++
        ProfileManager.removeListener(this)
        GroupManager.removeListener(this)
        list?.adapter = null
        list = null
        status = null
        super.onDestroyView()
    }
    private fun refresh() {
        val lifecycle = viewLifecycleOwnerLiveData.value ?: return
        lifecycle.lifecycleScope.launch(Dispatchers.Main.immediate) {
            val ticket = ++generation
            try {
                val snapshot = withContext(Dispatchers.IO) { PreferredGroupStore.rows(groupId) }
                if (ticket != generation) return@launch
                owner = snapshot.first
                submitRows(snapshot.second)
                owner?.configBean?.preferredIntervalSeconds?.takeIf { it > 0 }?.let {
                    preferredTestResults.setManualPriorityInterval(it * 1000L)
                }
                status?.apply {
                    text = when {
                        owner == null -> "优选容器已删除"
                        rows.isEmpty() -> "暂无成员，请长按分组标签编辑引用"
                        else -> ""
                    }
                    visibility = if (text.isEmpty()) View.GONE else View.VISIBLE
                }
            } catch (e: CancellationException) { throw e }
            catch (e: Exception) {
                if (ticket != generation) return@launch
                owner = null
                submitRows(emptyList())
                status?.apply { text = "读取失败，请重新打开分组"; visibility = View.VISIBLE }
            }
        }
    }
    private fun showDoubleColumnMenu(anchor: View, node: ProxyEntity) {
        PopupMenu(requireContext(), anchor).apply {
            menuInflater.inflate(R.menu.double_column_item_menu, menu)
            val ownerId = owner?.id ?: 0L
            val active = node.id in activeMemberIds
            val actions = nodeDiagnosticActions(
                DataStore.serviceState, ownerId, DataStore.selectedProxy, DataStore.currentProfile,
            )
            menu.findItem(R.id.action_ip_quality).isVisible = active && actions.qualityVisible
            menu.findItem(R.id.action_speed_test).isVisible = active && actions.speedVisible
            setForceShowIcon(true)
            setOnMenuItemClickListener { item ->
                when (item.itemId) {
                    R.id.action_ip_quality -> (parentFragment as? ConfigurationFragment)
                        ?.showIPQualityForProfile(ownerId)
                    R.id.action_speed_test -> (parentFragment as? ConfigurationFragment)
                        ?.startSpeedTestForProfile(ownerId, 1)
                    R.id.action_edit -> sourceAction(node, R.id.edit)
                    R.id.action_share -> sourceAction(node, R.id.share)
                    R.id.action_delete -> sourceAction(node, R.id.remove)
                    else -> return@setOnMenuItemClickListener false
                }
                true
            }
            show()
        }
    }

    private fun sourceAction(node: ProxyEntity, action: Int) {
        viewLifecycleOwner.lifecycleScope.launch {
            try {
                val source = withContext(Dispatchers.IO) { SagerDatabase.proxyDao.getById(node.id) }
                    ?: return@launch
                when (action) {
                    R.id.edit -> {
                        if (source.type == ProxyEntity.TYPE_XHTTP) {
                            Toast.makeText(requireContext(), R.string.special_protocol_not_editable, Toast.LENGTH_SHORT).show()
                            return@launch
                        }
                        val subscription = withContext(Dispatchers.IO) {
                            SagerDatabase.groupDao.getById(source.groupId)?.type == GroupType.SUBSCRIPTION
                        }
                        Toast.makeText(requireContext(), "编辑原节点，所有引用同步生效", Toast.LENGTH_SHORT).show()
                        startActivity(source.settingIntent(requireContext(), subscription))
                    }
                    R.id.share -> {
                        val link = source.requireBean().toUniversalLink()
                        startActivity(Intent.createChooser(Intent(Intent.ACTION_SEND).apply {
                            type = "text/plain"; putExtra(Intent.EXTRA_TEXT, link)
                        }, "分享原节点"))
                    }
                    R.id.remove -> {
                        val updated = withContext(Dispatchers.IO) {
                            PreferredGroupStore.removeReference(groupId, source.id).also {
                                ProfileManager.postUpdate(it)
                                GroupManager.postReload(groupId)
                            }
                        }
                        Toast.makeText(requireContext(), "已移除优选引用，原节点保留；重新连接后生效", Toast.LENGTH_SHORT).show()
                        refresh()
                    }
                }
            } catch (e: CancellationException) { throw e }
            catch (_: Exception) {
                Toast.makeText(requireContext(), "操作失败，请刷新后重试", Toast.LENGTH_SHORT).show()
            }
        }
    }

    private fun connect() {
        val profile = owner ?: return
        if (requireArguments().getBoolean("select")) {
            (requireActivity() as ConfigurationFragment.SelectCallback).returnProfile(profile.id)
            return
        }
        val parent = parentFragment as? ConfigurationFragment
        if (parent != null) {
            parent.selectPreferredGroup(groupId)
            return
        }
        viewLifecycleOwner.lifecycleScope.launch {
            val previous = DataStore.selectedProxy
            DataStore.selectedProxy = profile.id
            DataStore.selectedGroup = groupId
            withContext(Dispatchers.IO) {
                ProfileManager.postUpdate(previous, noTraffic = true)
                ProfileManager.postUpdate(profile, noTraffic = true)
            }
            if (previous != profile.id && DataStore.serviceState.canStop) SagerNet.reloadService()
            refresh()
        }
    }
    override suspend fun onAdd(profile: ProxyEntity) = refresh()
    override suspend fun onUpdated(profile: ProxyEntity, noTraffic: Boolean) = refresh()
    override suspend fun onUpdated(data: List<TrafficData>) = Unit
    override suspend fun onRemoved(groupId: Long, profileId: Long) = refresh()
    override suspend fun groupAdd(group: ProxyGroup) = refresh()
    override suspend fun groupUpdated(group: ProxyGroup) = refresh()
    override suspend fun groupUpdated(groupId: Long) = refresh()
    override suspend fun groupRemoved(groupId: Long) = refresh()
}
