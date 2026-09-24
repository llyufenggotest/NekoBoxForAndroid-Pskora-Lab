package io.nekohasekai.sagernet.ui

import android.annotation.SuppressLint
import android.content.DialogInterface
import android.content.Intent
import android.graphics.Color
import android.graphics.drawable.ColorDrawable
import android.net.Uri
import android.os.Bundle
import android.os.SystemClock
import android.provider.OpenableColumns
import android.text.SpannableStringBuilder
import android.text.Spanned.SPAN_EXCLUSIVE_EXCLUSIVE
import android.text.style.ForegroundColorSpan
import android.view.Gravity
import android.view.KeyEvent
import android.view.LayoutInflater
import android.view.Menu
import android.view.MenuItem
import android.view.MotionEvent
import android.view.View
import android.view.ViewConfiguration
import android.view.ViewGroup
import android.widget.ImageView
import android.widget.LinearLayout
import android.widget.PopupWindow
import android.widget.TextView
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.widget.PopupMenu
import androidx.appcompat.widget.SearchView
import androidx.appcompat.widget.SwitchCompat
import androidx.appcompat.widget.Toolbar
import androidx.core.graphics.ColorUtils
import androidx.core.net.toUri
import androidx.core.view.isGone
import androidx.core.view.isVisible
import androidx.core.view.size
import kotlinx.coroutines.delay
import androidx.fragment.app.Fragment
import androidx.lifecycle.lifecycleScope
import androidx.preference.PreferenceDataStore
import androidx.recyclerview.widget.ItemTouchHelper
import androidx.recyclerview.widget.LinearLayoutManager
import androidx.recyclerview.widget.RecyclerView
import androidx.viewpager2.adapter.FragmentStateAdapter
import androidx.viewpager2.widget.ViewPager2
import com.google.android.material.card.MaterialCardView
import com.google.android.material.dialog.MaterialAlertDialogBuilder
import com.google.android.material.tabs.TabLayout
import com.google.android.material.tabs.TabLayoutMediator
import io.nekohasekai.sagernet.GroupOrder
import io.nekohasekai.sagernet.GroupType
import io.nekohasekai.sagernet.Key
import io.nekohasekai.sagernet.R
import io.nekohasekai.sagernet.SagerNet
import io.nekohasekai.sagernet.aidl.TrafficData
import io.nekohasekai.sagernet.aidl.ISagerNetService
import io.nekohasekai.sagernet.bg.BaseService
import io.nekohasekai.sagernet.bg.proto.TUN_NET_CLIENT_VERSION
import io.nekohasekai.sagernet.bg.proto.UrlTest
import io.nekohasekai.sagernet.database.DataStore
import io.nekohasekai.sagernet.database.GroupManager
import io.nekohasekai.sagernet.database.ProfileManager
import io.nekohasekai.sagernet.database.PreferredGroupStore
import io.nekohasekai.sagernet.database.ProxyEntity
import io.nekohasekai.sagernet.database.ProxyGroup
import io.nekohasekai.sagernet.database.SagerDatabase
import io.nekohasekai.sagernet.database.preference.OnPreferenceDataStoreChangeListener
import io.nekohasekai.sagernet.databinding.LayoutProfileListBinding
import io.nekohasekai.sagernet.databinding.LayoutProgressListBinding
import io.nekohasekai.sagernet.fmt.AbstractBean
import io.nekohasekai.sagernet.fmt.buildConfig
import io.nekohasekai.sagernet.fmt.oppa.parseOppaProvider
import io.nekohasekai.sagernet.fmt.toUniversalLink
import io.nekohasekai.sagernet.fmt.v2ray.VMessBean
import io.nekohasekai.sagernet.fmt.v2ray.isTunNet
import io.nekohasekai.sagernet.fmt.v2ray.tunNetSelection
import io.nekohasekai.sagernet.group.GroupUpdater
import io.nekohasekai.sagernet.group.RawUpdater
import io.nekohasekai.sagernet.ktx.FixedLinearLayoutManager
import io.nekohasekai.sagernet.ktx.FixedGridLayoutManager
import io.nekohasekai.sagernet.ktx.Logs
import io.nekohasekai.sagernet.ktx.SubscriptionFoundException
import io.nekohasekai.sagernet.ktx.alert
import io.nekohasekai.sagernet.ktx.app
import io.nekohasekai.sagernet.ktx.dp2px
import io.nekohasekai.sagernet.ktx.getColorAttr
import io.nekohasekai.sagernet.ktx.getColour
import io.nekohasekai.sagernet.ktx.isIpAddress
import io.nekohasekai.sagernet.ktx.onMainDispatcher
import io.nekohasekai.sagernet.ktx.readableMessage
import io.nekohasekai.sagernet.ktx.runOnDefaultDispatcher
import io.nekohasekai.sagernet.ktx.runOnLifecycleDispatcher
import io.nekohasekai.sagernet.ktx.runOnMainDispatcher
import io.nekohasekai.sagernet.ktx.scrollTo
import io.nekohasekai.sagernet.ktx.showAllowingStateLoss
import io.nekohasekai.sagernet.ktx.snackbar
import io.nekohasekai.sagernet.ktx.startFilesForResult
import io.nekohasekai.sagernet.ktx.tryToShow
import io.nekohasekai.sagernet.ktx.USER_AGENT
import io.nekohasekai.sagernet.plugin.PluginManager
import io.nekohasekai.sagernet.ui.profile.ChainSettingsActivity
import io.nekohasekai.sagernet.ui.profile.HttpSettingsActivity
import io.nekohasekai.sagernet.ui.profile.HysteriaSettingsActivity
import io.nekohasekai.sagernet.ui.profile.JuicitySettingsActivity
import io.nekohasekai.sagernet.ui.profile.MieruSettingsActivity
import io.nekohasekai.sagernet.ui.profile.NaiveSettingsActivity
import io.nekohasekai.sagernet.ui.profile.OppaSettingsActivity
import io.nekohasekai.sagernet.ui.profile.SSHSettingsActivity
import io.nekohasekai.sagernet.ui.profile.ShadowsocksSettingsActivity
import io.nekohasekai.sagernet.ui.profile.ShadowsocksRSettingsActivity
import io.nekohasekai.sagernet.ui.profile.SnellSettingsActivity
import io.nekohasekai.sagernet.ui.profile.SocksSettingsActivity
import io.nekohasekai.sagernet.ui.profile.TrojanGoSettingsActivity
import io.nekohasekai.sagernet.ui.profile.TrojanSettingsActivity
import io.nekohasekai.sagernet.ui.profile.TuicSettingsActivity
import io.nekohasekai.sagernet.ui.profile.VMessSettingsActivity
import io.nekohasekai.sagernet.ui.profile.WireGuardSettingsActivity
import io.nekohasekai.sagernet.widget.QRCodeDialog
import io.nekohasekai.sagernet.widget.UndoSnackbarManager
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.DelicateCoroutinesApi
import kotlinx.coroutines.NonCancellable
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.cancelAndJoin
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.isActive
import kotlinx.coroutines.joinAll
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull
import moe.matsuri.nb4a.Protocols
import moe.matsuri.nb4a.Protocols.getProtocolColor
import moe.matsuri.nb4a.proxy.anytls.AnyTLSSettingsActivity
import moe.matsuri.nb4a.proxy.config.ConfigSettingActivity
import moe.matsuri.nb4a.proxy.shadowtls.ShadowTLSSettingsActivity
import moe.matsuri.nb4a.ui.ConnectionTestNotification
import moe.matsuri.nb4a.utils.Util
import libcore.Libcore
import okhttp3.internal.closeQuietly
import java.net.InetSocketAddress
import java.net.Socket
import java.net.URLDecoder
import java.net.UnknownHostException
import java.io.File
import java.util.UUID
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.ConcurrentLinkedQueue
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicLong
import java.util.zip.ZipInputStream
import kotlin.collections.set
import androidx.appcompat.app.AlertDialog
import io.nekohasekai.sagernet.database.SubscriptionBean
import kotlin.math.abs

internal fun <T> selectProfilesForQuery(
    query: String,
    currentGroupId: Long,
    allProfiles: List<T>,
    groupId: (T) -> Long,
    matches: (T, String) -> Boolean,
): List<T> {
    val normalized = query.trim()
    return if (normalized.isEmpty()) {
        allProfiles.filter { groupId(it) == currentGroupId }
    } else {
        allProfiles.filter { matches(it, normalized) }
    }
}

class ConfigurationFragment @JvmOverloads constructor(
    val select: Boolean = false, val selectedItem: ProxyEntity? = null, val titleRes: Int = 0
) : ToolbarFragment(R.layout.layout_group_list),
    PopupMenu.OnMenuItemClickListener,
    Toolbar.OnMenuItemClickListener,
    SearchView.OnQueryTextListener,
    OnPreferenceDataStoreChangeListener {

    interface SelectCallback {
        fun returnProfile(profileId: Long)
    }

    lateinit var adapter: GroupPagerAdapter
    lateinit var tabLayout: TabLayout
    lateinit var groupPager: ViewPager2

    val alwaysShowAddress by lazy { DataStore.alwaysShowAddress }

    @Volatile
    private var selectedProxySnapshot = selectedItem?.id ?: 0L

    @Volatile
    private var currentProfileSnapshot = 0L

    @Volatile
    private var serviceStartedSnapshot = DataStore.serviceState.started
    private lateinit var dashboardConnectionButton: MaterialCardView
    private lateinit var dashboardConnectionIcon: ImageView
    private var dashboardPulse: io.nekohasekai.sagernet.widget.ConnectionRing? = null
    private lateinit var dashboardState: TextView
    private lateinit var dashboardAction: TextView
    private lateinit var dashboardSessionUpload: TextView
    private lateinit var dashboardSessionDownload: TextView
    private lateinit var dashboardTestCard: MaterialCardView
    private lateinit var dashboardTestStatus: TextView
    private lateinit var dashboardLatency: TextView
    private lateinit var dashboardUpload: TextView
    private lateinit var dashboardDownload: TextView

    private data class ProfileStateSnapshot(
        val selectedProxy: Long,
        val currentProfile: Long,
        val serviceStarted: Boolean,
    )

    private val profileStateRequests = Channel<Long>(Channel.CONFLATED)
    private val profileStateGeneration = AtomicLong()
    private val profileStateInitialized = CompletableDeferred<Unit>()

    fun refreshProfileState() {
        lifecycleScope.launch(Dispatchers.Main.immediate) {
            updateDashboardState()
            val generation = profileStateGeneration.incrementAndGet()
            profileStateRequests.trySend(generation)
        }
    }

    private var preferredNameJob: kotlinx.coroutines.Job? = null
    override fun onResume() {
        super.onResume()
        preferredNameJob = viewLifecycleOwner.lifecycleScope.launch {
            while (true) {
                updateDashboardState()
                kotlinx.coroutines.delay(1000)
            }
        }
    }
    override fun onPause() {
        preferredNameJob?.cancel()
        preferredNameJob = null
        super.onPause()
    }

    private fun updateDashboardState() {
        if (!::dashboardConnectionButton.isInitialized) return
        val state = DataStore.serviceState
        val health = (activity as? MainActivity)?.dashboardHealth
        val tone = health?.tone ?: DashboardHealth.Tone.GRAY
        val connected = tone == DashboardHealth.Tone.GREEN
        val busy = state == BaseService.State.Connecting || state == BaseService.State.Stopping
        dashboardConnectionButton.isEnabled = !busy
        dashboardConnectionButton.setCardBackgroundColor(
            androidx.core.content.ContextCompat.getColor(requireContext(), tone.background)
        )
        dashboardConnectionIcon.setImageResource(
            when (state) {
                BaseService.State.Connecting -> R.drawable.ic_service_connecting
                BaseService.State.Connected -> R.drawable.ic_service_connected
                BaseService.State.Stopping -> R.drawable.ic_service_stopping
                else -> R.drawable.ic_service_idle
            }
        )
        dashboardConnectionIcon.imageTintList = android.content.res.ColorStateList.valueOf(
            androidx.core.content.ContextCompat.getColor(requireContext(), tone.foreground)
        )
        if (connected) startDashboardPulse() else stopDashboardPulse()
        dashboardState.text = when {
            tone == DashboardHealth.Tone.RED -> "连接失败"
            state.connected && !connected -> "正在测试…"
            else -> when (state) {
            BaseService.State.Connecting -> "正在连接…"
            BaseService.State.Connected -> "已连接"
            BaseService.State.Stopping -> "正在断开…"
            else -> "等待连接"
            }
        }
        dashboardAction.text = when (state) {
            BaseService.State.Connecting -> "正在建立 VPN"
            BaseService.State.Connected -> "点击断开 VPN"
            BaseService.State.Stopping -> "正在关闭 VPN"
            else -> "点击连接 VPN"
        }
        if (!state.connected && ::dashboardSessionUpload.isInitialized) {
            dashboardSessionUpload.text = "0 B"
            dashboardSessionDownload.text = "0 B"
        }
        val profileId = if (state.started && DataStore.currentProfile > 0) DataStore.currentProfile else DataStore.selectedProxy
        updateDashboardConnectionTest(profileId, health?.result, refreshState = false)
        val expectedView = view ?: return
        viewLifecycleOwner.lifecycleScope.launch {
            val name = withContext(Dispatchers.IO) {
                ProfileManager.getProfile(profileId)?.let { profile ->
                    if (state.connected && profile.configBean?.type == 2) {
                        "${profile.displayName()} → ${preferredRuntimeLabel((activity as? MainActivity)?.connection?.service, profileId)}"
                    } else profile.displayName()
                } ?: "未选择节点"
            }
            val latestId = if (DataStore.serviceState.started && DataStore.currentProfile > 0) DataStore.currentProfile else DataStore.selectedProxy
            if (view !== expectedView || latestId != profileId || DataStore.serviceState != state) return@launch
            expectedView.findViewById<TextView>(R.id.dashboard_test_profile_name).text = name
        }
    }

    private fun startDashboardPulse() {
        if (dashboardPulse == null) dashboardPulse = io.nekohasekai.sagernet.widget.ConnectionRing(
            dashboardConnectionButton, viewLifecycleOwner,
        )
        dashboardPulse?.setConnected(true)
    }

    private fun stopDashboardPulse() {
        dashboardPulse?.dispose()
        dashboardPulse = null
    }

    private var dashboardPeakUpload = 0L
    private var dashboardPeakDownload = 0L
    private val nodeRates = mutableMapOf<Long, Pair<Long, Long>>()
    fun refreshDiagnosticCards() {
        if (!::adapter.isInitialized) return
        adapter.groupFragments.values.forEach { fragment ->
            fragment.adapter?.let { child ->
                if (child.itemCount > 0) child.notifyItemRangeChanged(0, child.itemCount, Unit)
            }
        }
        adapter.preferredGroupFragments.values.forEach { it.refreshDiagnosticCards() }
    }

    fun updateSpeedTestProgress(profileId: Long, phase: String, currentMBps: Double, peakMBps: Double) {
        val previous = speedTestRows[profileId] ?: SpeedTestCardState()
        speedTestRows[profileId] = when (phase) {
            "download" -> previous.copy(
                phase = phase,
                downloadCurrent = currentMBps,
                downloadPeak = maxOf(previous.downloadPeak, peakMBps),
                running = true,
                error = "",
            )
            "upload" -> previous.copy(
                phase = phase,
                uploadCurrent = currentMBps,
                uploadPeak = maxOf(previous.uploadPeak, peakMBps),
                running = true,
                error = "",
            )
            else -> previous.copy(phase = phase, running = true, error = "")
        }
        refreshDiagnosticCards()
        renderSpeedTestDialog(profileId)
    }

    fun updateSpeedTestComplete(profileId: Long, downloadMBps: Double, uploadMBps: Double) {
        val previous = speedTestRows[profileId] ?: SpeedTestCardState()
        speedTestRows[profileId] = previous.copy(
            phase = "complete",
            running = false,
            downloadAverage = downloadMBps,
            uploadAverage = uploadMBps,
            error = "",
        )
        refreshDiagnosticCards()
        renderSpeedTestDialog(profileId)
    }

    fun updateSpeedTestError(profileId: Long, message: String) {
        speedTestRows[profileId] = (speedTestRows[profileId] ?: SpeedTestCardState()).copy(
            phase = "error", running = false, error = message,
        )
        refreshDiagnosticCards()
        renderSpeedTestDialog(profileId)
    }

    private data class SpeedTestCardState(
        val phase: String = "idle",
        val downloadCurrent: Double = 0.0,
        val uploadCurrent: Double = 0.0,
        val downloadPeak: Double = 0.0,
        val uploadPeak: Double = 0.0,
        val downloadAverage: Double = 0.0,
        val uploadAverage: Double = 0.0,
        val running: Boolean = false,
        val error: String = "",
    )

    private val speedTestRows = mutableMapOf<Long, SpeedTestCardState>()
    private var speedTestDialog: androidx.appcompat.app.AlertDialog? = null
    private var speedTestDialogProfileId = 0L
    private var speedTestDialogPhase: TextView? = null
    private var speedTestDialogUpload: TextView? = null
    private var speedTestDialogDownload: TextView? = null
    private var speedTestDialogProgress: android.widget.ProgressBar? = null
    private val qualityTiers = mutableMapOf<Long, String>()

    fun qualityTier(profileId: Long): String? = qualityTiers[profileId]

    fun nodeRatesFor(profileId: Long): Pair<Long, Long>? = nodeRates[profileId]

    fun speedDisplayFor(profileId: Long): Pair<String, String>? {
        val tested = speedTestRows[profileId] ?: return null
        if (tested.error.isNotEmpty()) return "" to ""
        val upload = when {
            tested.running && tested.phase == "upload" -> "↑${"%.2f".format(tested.uploadCurrent)} MB/s"
            tested.running -> ""
            else -> "↑${"%.2f".format(tested.uploadAverage)} MB/s"
        }
        val download = when {
            tested.running && tested.phase == "download" -> "↓${"%.2f".format(tested.downloadCurrent)} MB/s"
            tested.running && tested.downloadPeak > 0.0 -> "↓${"%.2f".format(tested.downloadPeak)} MB/s"
            tested.running -> ""
            else -> "↓${"%.2f".format(tested.downloadAverage)} MB/s"
        }
        return upload to download
    }

    fun clearDiagnosticSessions() {
        nodeRates.clear()
        speedTestRows.clear()
        qualityTiers.clear()
        dashboardPeakUpload = 0L
        dashboardPeakDownload = 0L
        refreshDiagnosticCards()
    }

    private fun service(): ISagerNetService? = (activity as? MainActivity)?.connection?.service

    fun startSpeedTestForProfile(profileId: Long, streams: Int) {
        if (DataStore.groupLayoutMode == 1) showSpeedTestDialog(profileId)
        runOnDefaultDispatcher {
            val result = runCatching { service()?.startSpeedTest(profileId, streams) ?: "Service disconnected" }
                .getOrElse { it.readableMessage }
            if (result.isNotEmpty()) onMainDispatcher {
                updateSpeedTestError(profileId, result)
                snackbar(result).show()
            }
        }
    }

    private fun showSpeedTestDialog(profileId: Long) {
        speedTestDialog?.dismiss()
        val content = LayoutInflater.from(requireContext()).inflate(R.layout.dialog_speed_test, null)
        val dialog = androidx.appcompat.app.AlertDialog.Builder(requireContext()).setView(content).create()
        speedTestDialogProfileId = profileId
        speedTestDialogPhase = content.findViewById(R.id.speed_test_dialog_phase)
        speedTestDialogUpload = content.findViewById(R.id.speed_test_dialog_upload)
        speedTestDialogDownload = content.findViewById(R.id.speed_test_dialog_download)
        speedTestDialogProgress = content.findViewById(R.id.speed_test_dialog_progress)
        content.findViewById<TextView>(R.id.speed_test_dialog_node).text =
            ProfileManager.getProfile(profileId)?.displayName().orEmpty()
        content.findViewById<View>(R.id.speed_test_dialog_close).setOnClickListener { dialog.dismiss() }
        dialog.setOnDismissListener {
            if (speedTestDialog === dialog) {
                speedTestDialog = null
                speedTestDialogProfileId = 0L
            }
        }
        dialog.setOnShowListener { dialog.window?.setBackgroundDrawable(ColorDrawable(Color.TRANSPARENT)) }
        speedTestDialog = dialog
        dialog.show()
        renderSpeedTestDialog(profileId)
    }

    private fun renderSpeedTestDialog(profileId: Long) {
        if (speedTestDialogProfileId != profileId || speedTestDialog?.isShowing != true) return
        val state = speedTestRows[profileId]
        speedTestDialogUpload?.text = when {
            state == null -> "↑ -- MB/s"
            state.running && state.phase == "upload" -> "↑ ${"%.2f".format(state.uploadCurrent)} MB/s"
            !state.running -> "↑ ${"%.2f".format(state.uploadAverage)} MB/s"
            else -> "↑ -- MB/s"
        }
        speedTestDialogDownload?.text = when {
            state == null -> "↓ -- MB/s"
            state.running && state.downloadPeak > 0.0 ->
                "↓ ${"%.2f".format(state.downloadCurrent.takeIf { it > 0.0 } ?: state.downloadPeak)} MB/s"
            !state.running -> "↓ ${"%.2f".format(state.downloadAverage)} MB/s"
            else -> "↓ -- MB/s"
        }
        speedTestDialogPhase?.text = when {
            state == null -> "准备测速…"
            state.error.isNotEmpty() -> "测速失败：${state.error}"
            state.running && state.phase == "download" -> "正在测试下载速度…"
            state.running && state.phase == "upload" -> "正在测试上传速度…"
            else -> "测速完成"
        }
        speedTestDialogProgress?.visibility = if (state?.running == false) View.INVISIBLE else View.VISIBLE
    }

    fun showIPQualityForProfile(profileId: Long) {
        val content = LayoutInflater.from(requireContext()).inflate(R.layout.dialog_ip_quality, null)
        val dialog = androidx.appcompat.app.AlertDialog.Builder(requireContext())
            .setView(content)
            .create()
        content.findViewById<View>(R.id.ip_quality_dialog_close).setOnClickListener { dialog.dismiss() }
        val summary = content.findViewById<TextView>(R.id.ip_quality_dialog_summary)
        val details = content.findViewById<TextView>(R.id.ip_quality_dialog_content)
        summary.setText(R.string.ip_quality_loading)
        details.text = ""
        dialog.setOnShowListener {
            dialog.window?.setBackgroundDrawable(ColorDrawable(Color.TRANSPARENT))
        }
        dialog.show()
        viewLifecycleOwner.lifecycleScope.launch {
            val raw = withContext(Dispatchers.IO) {
                runCatching { service()?.queryIpQuality(profileId) ?: error("Service disconnected") }
            }
            if (!isAdded || !dialog.isShowing) return@launch
            raw.onSuccess { json ->
                val value = org.json.JSONObject(json)
                val score = value.optInt("score")
                val tier = value.optString("scoreTier")
                qualityTiers[profileId] = tier
                refreshDiagnosticCards()
                summary.text = "${value.optString("ip")} · ${ipAttributeChinese(value.optString("ipAttribute"))}"
                val locations = value.optJSONArray("locations")
                val locationText = buildList {
                    if (locations != null) for (index in 0 until locations.length()) {
                        val place = locations.getJSONObject(index)
                        val flag = countryFlag(place.optString("countryCode"))
                        val detail = listOf(
                            place.optString("country"),
                            place.optString("region"),
                            place.optString("city"),
                        ).filter { it.isNotBlank() }.joinToString(" · ")
                        add("$flag ${place.optString("provider")}：$detail")
                    }
                }.joinToString("\n")
                details.text = getString(
                    R.string.ip_quality_result,
                    value.optString("asn"),
                    ipSourceChinese(value.optString("ipSource")),
                    ipAttributeChinese(value.optString("ipAttribute")),
                    score,
                    ipQualityTierChinese(tier),
                    locationText,
                )
                content.findViewById<android.widget.ImageView>(R.id.ip_quality_dialog_leaf)
                    .setColorFilter(requireContext().getColour(when (tier) {
                        "green" -> R.color.material_green_500
                        "yellow" -> R.color.material_amber_500
                        else -> R.color.material_red_500
                    }))
            }.onFailure {
                summary.setText(R.string.ip_quality_failed)
                details.text = it.readableMessage
            }
        }
    }

    fun updateDashboardSpeed(
        targetProfileId: Long,
        txRate: Long,
        rxRate: Long,
        txTotal: Long,
        rxTotal: Long,
    ) {
        if (!::dashboardUpload.isInitialized || !isAdded) return
        val activeProfileId = if (DataStore.serviceState.started && DataStore.currentProfile > 0) {
            DataStore.currentProfile
        } else {
            DataStore.selectedProxy
        }
        if (targetProfileId != activeProfileId) return
        dashboardPeakUpload = maxOf(dashboardPeakUpload, txRate)
        dashboardPeakDownload = maxOf(dashboardPeakDownload, rxRate)
        nodeRates[targetProfileId] = txRate to rxRate
        dashboardUpload.text = android.text.format.Formatter.formatFileSize(requireContext(), txRate) + "/s"
        dashboardDownload.text = android.text.format.Formatter.formatFileSize(requireContext(), rxRate) + "/s"
        dashboardSessionUpload.text = android.text.format.Formatter.formatFileSize(requireContext(), txTotal)
        dashboardSessionDownload.text = android.text.format.Formatter.formatFileSize(requireContext(), rxTotal)
        adapter.groupFragments.values.forEach { fragment ->
            fragment.adapter?.refreshProfileState(setOf(targetProfileId))
        }
        adapter.preferredGroupFragments.values.forEach { it.refreshDiagnosticCards() }
    }

    fun updateDashboardConnectionTest(targetProfileId: Long, result: DashboardConnectionTestResult?, refreshState: Boolean = true) {
        if (!::dashboardLatency.isInitialized) return
        val activeProfileId = if (DataStore.serviceState.started && DataStore.currentProfile > 0) {
            DataStore.currentProfile
        } else {
            DataStore.selectedProxy
        }
        if (targetProfileId != activeProfileId) return
        val testing = result is DashboardConnectionTestResult.Testing
        dashboardTestCard.isEnabled = !testing
        if (refreshState) updateDashboardState()
        when (result) {
            null -> {
                dashboardTestStatus.text = "点击测试  ›"
                dashboardLatency.text = "待测"
            }
            DashboardConnectionTestResult.Testing -> {
                dashboardTestStatus.text = "测试中…"
                dashboardLatency.text = "测试中"
            }
            is DashboardConnectionTestResult.Success -> {
                dashboardTestStatus.text = "成功 · 可再次测试  ›"
                dashboardLatency.text = "${result.elapsedMs} ms"
            }
            DashboardConnectionTestResult.Timeout -> {
                dashboardTestStatus.text = "超时 · 点击重试  ›"
                dashboardLatency.text = "超时"
            }
            is DashboardConnectionTestResult.Failure -> {
                dashboardTestStatus.text = "失败：${result.reason} · 点击重试  ›"
                dashboardLatency.text = "失败"
            }
        }
    }

    private fun updateSelectedProxySnapshot(profileId: Long) {
        val generation = profileStateGeneration.incrementAndGet()
        updateProfileStateSnapshots(
            profileId,
            currentProfileSnapshot,
            DataStore.serviceState.started,
        )
        profileStateRequests.trySend(generation)
    }

    private fun startProfileStateActor() {
        lifecycleScope.launch(Dispatchers.Main.immediate) {
            for (generation in profileStateRequests) {
                val snapshot = try {
                    withContext(Dispatchers.IO) {
                        ProfileStateSnapshot(
                            selectedProxy = selectedItem?.id ?: DataStore.selectedProxy,
                            currentProfile = DataStore.currentProfile,
                            serviceStarted = DataStore.serviceState.started,
                        )
                    }
                } catch (e: CancellationException) {
                    throw e
                } catch (e: Exception) {
                    Logs.w(e)
                    if (generation == profileStateGeneration.get()) {
                        profileStateInitialized.complete(Unit)
                    }
                    continue
                }
                if (generation != profileStateGeneration.get()) continue
                updateProfileStateSnapshots(
                    snapshot.selectedProxy,
                    snapshot.currentProfile,
                    snapshot.serviceStarted,
                )
                profileStateInitialized.complete(Unit)
            }
        }
    }

    private fun updateProfileStateSnapshots(
        selectedProxy: Long,
        currentProfile: Long,
        serviceStarted: Boolean,
    ) {
        val changedIds = mutableSetOf<Long>()
        if (selectedProxySnapshot != selectedProxy) {
            changedIds.add(selectedProxySnapshot)
            changedIds.add(selectedProxy)
        }
        if (currentProfileSnapshot != currentProfile) {
            changedIds.add(currentProfileSnapshot)
            changedIds.add(currentProfile)
        }
        if (serviceStartedSnapshot != serviceStarted) {
            changedIds.add(selectedProxySnapshot)
            changedIds.add(currentProfileSnapshot)
            changedIds.add(selectedProxy)
            changedIds.add(currentProfile)
        }
        changedIds.removeAll { it <= 0L }

        selectedProxySnapshot = selectedProxy
        currentProfileSnapshot = currentProfile
        serviceStartedSnapshot = serviceStarted
        // Also refresh the dashboard on node-selection and selector callbacks.
        if (changedIds.isNotEmpty()) updateDashboardState()

        if (changedIds.isEmpty() || !::adapter.isInitialized) return
        adapter.groupFragments.values.forEach { fragment ->
            fragment.adapter?.refreshProfileState(changedIds)
        }
    }

    private fun isSelectedProfile(profileId: Long) = selectedProxySnapshot == profileId

    private fun isCurrentProfile(profileId: Long) = currentProfileSnapshot == profileId

    private fun isCurrentGroupPagerAdapter(candidate: GroupPagerAdapter): Boolean {
        return ::adapter.isInitialized && adapter === candidate
    }

    fun getCurrentGroupFragment(): GroupFragment? {
        return try {
            childFragmentManager.findFragmentByTag("f" + DataStore.selectedGroup) as GroupFragment?
        } catch (e: Exception) {
            Logs.e(e)
            null
        }
    }

    fun switchAllGroupFragmentsLayout() {
        adapter.groupFragments.values.forEach { fragment ->
            if (fragment.isAdded && fragment.view != null) {
                fragment.switchLayoutMode()
            }
        }
        adapter.preferredGroupFragments.values.forEach { fragment ->
            if (fragment.isAdded && fragment.view != null) {
                fragment.switchLayoutMode()
            }
        }
    }

    fun refreshAllGroupFragmentsCardStyle() {
        adapter.groupFragments.values.forEach { fragment ->
            if (fragment.isAdded && fragment.view != null) {
                fragment.adapter?.notifyDataSetChanged()
            }
        }
    }

    private var lastGroupTabTapAt = 0L
    private var lastGroupTabTapIndex = -1

    private var suppressNextGroupLongPress = false

    private fun installGroupTabInteractions(tab: TabLayout.Tab, position: Int) {
        tab.view.setOnLongClickListener {
            if (suppressNextGroupLongPress) {
                suppressNextGroupLongPress = false
                return@setOnLongClickListener true
            }
            if (position !in adapter.groupList.indices) return@setOnLongClickListener true
            showDashboardGroupMenu(tab.view, adapter.groupList[position], position)
            true
        }
        tab.view.setOnTouchListener { _, event ->
            if (event.actionMasked == MotionEvent.ACTION_UP) {
                val now = SystemClock.elapsedRealtime()
                if (lastGroupTabTapIndex == position && now - lastGroupTabTapAt in 1..350) {
                    lastGroupTabTapAt = 0L
                    lastGroupTabTapIndex = -1
                    suppressNextGroupLongPress = true
                    tab.view.postDelayed({ suppressNextGroupLongPress = false }, 450L)
                    if (adapter.groupList.isEmpty()) return@setOnTouchListener true
                    val targetIndex = groupTabDoubleTapTarget(
                        event.rawX,
                        resources.displayMetrics.widthPixels,
                        adapter.groupList.lastIndex,
                    )
                    DataStore.selectedGroup = adapter.groupList[targetIndex].id
                    groupPager.setCurrentItem(targetIndex, false)
                    tabLayout.post { tabLayout.setScrollPosition(targetIndex, 0f, true) }
                    true
                } else {
                    lastGroupTabTapAt = now
                    lastGroupTabTapIndex = position
                    false
                }
            } else false
        }
    }

    private fun restoreGroupTabPosition(groupIndex: Int) {
        if (groupIndex !in adapter.groupList.indices) return
        groupPager.setCurrentItem(groupIndex, false)
        tabLayout.post {
            tabLayout.getTabAt(groupIndex)?.select()
            tabLayout.setScrollPosition(groupIndex, 0f, true)
        }
    }
    val updateSelectedCallback = object : ViewPager2.OnPageChangeCallback() {
        override fun onPageSelected(position: Int) {
            if (adapter.groupList.size > position) {
                adapter.selectedGroupIndex = position
                DataStore.selectedGroup = adapter.groupList[position].id
            }
        }
    }

    /** Used by reference rows and the group menu; never selects a candidate leaf. */
    fun selectPreferredGroup(groupId: Long) {
        viewLifecycleOwner.lifecycleScope.launch {
            val owner = withContext(Dispatchers.IO) { PreferredGroupStore.container(groupId) }
                ?: return@launch
            val previous = DataStore.selectedProxy
            DataStore.selectedProxy = owner.id
            DataStore.selectedGroup = groupId
            updateSelectedProxySnapshot(owner.id)
            withContext(Dispatchers.IO) {
                ProfileManager.postUpdate(previous, noTraffic = true)
                ProfileManager.postUpdate(owner, noTraffic = true)
            }
            if (previous != owner.id && DataStore.serviceState.canStop) SagerNet.reloadService()
        }
    }

    private fun showDashboardGroupMenu(anchor: View, group: ProxyGroup, position: Int) {
        val subscriptionLink = group.subscription?.link.orEmpty()
        val capabilities = dashboardGroupMenuCapabilities(
            ungrouped = group.ungrouped,
            isSubscription = group.type == GroupType.SUBSCRIPTION,
            subscriptionUrlPresent = subscriptionLink.isNotBlank(),
            groupCount = adapter.groupList.size,
            isUpdating = group.id in GroupUpdater.updating,
        )
        val context = requireContext()
        val content = LinearLayout(context).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp2px(8), dp2px(6), dp2px(8), dp2px(6))
            background = androidx.appcompat.content.res.AppCompatResources.getDrawable(
                context, R.drawable.bg_dashboard_group_menu_glass
            )
            elevation = dp2px(10).toFloat()
        }
        lateinit var window: PopupWindow
        fun addAction(title: CharSequence, @androidx.annotation.DrawableRes icon: Int,
            enabled: Boolean = true, clickable: Boolean = true, action: () -> Unit) {
            if (!enabled) return
            val row = layoutInflater.inflate(R.layout.layout_group_action_outline, content, false)
            row.findViewById<android.widget.ImageView>(R.id.group_action_icon).setImageResource(icon)
            row.findViewById<TextView>(R.id.group_action_title).text = title
            row.contentDescription = title
            row.isEnabled = clickable
            row.alpha = if (clickable) 1f else 0.5f
            row.setOnClickListener { if (clickable) { window.dismiss(); action() } }
            content.addView(row)
        }
        addAction("使用此优选组（自动选择）", R.drawable.ic_group_action_use_outline, group.type == GroupType.PREFERRED) {
            selectPreferredGroup(group.id)
        }
        addAction(getString(R.string.edit), R.drawable.ic_group_action_edit_outline, capabilities.canEdit) {
            if (group.type == GroupType.PREFERRED) {
                viewLifecycleOwner.lifecycleScope.launch {
                    val owner = withContext(Dispatchers.IO) { PreferredGroupStore.container(group.id) }
                    owner?.let { startActivity(it.settingIntent(context, false)) }
                }
            } else {
                startActivity(Intent(context, GroupSettingsActivity::class.java).apply {
                    putExtra(GroupSettingsActivity.EXTRA_GROUP_ID, group.id)
                })
            }
        }
        addAction("复制订阅链接", R.drawable.ic_group_action_copy_outline, capabilities.canCopySubscriptionLink) {
            val success = subscriptionLink.isNotBlank() && SagerNet.trySetPrimaryClip(subscriptionLink)
            snackbar(if (success) "订阅链接已复制" else "复制订阅链接失败").show()
        }
        val updating = group.id in GroupUpdater.updating || group.id in pendingDashboardUpdates
        addAction(if (updating) "更新中…" else "更新订阅", R.drawable.ic_group_action_update_outline,
            group.type == GroupType.SUBSCRIPTION && group.subscription != null, !updating) {
            updateDashboardSubscription(group)
        }
        addAction("置顶", R.drawable.ic_group_action_top_outline) {
            runOnDefaultDispatcher {
                try {
                    val groups = SagerDatabase.groupDao.allGroups().toMutableList()
                    val index = groups.indexOfFirst { it.id == group.id }
                    if (index < 0) return@runOnDefaultDispatcher
                    moveItemToFrontAndReindex(groups, index) { item, order -> item.userOrder = order }
                    SagerDatabase.groupDao.updateGroups(groups)
                    onMainDispatcher {
                        if (isAdded && view != null) {
                            adapter.reload()
                            snackbar(R.string.group_pinned).show()
                        }
                    }
                } catch (error: Exception) {
                    Logs.w(error)
                    onMainDispatcher {
                        if (isAdded && view != null) snackbar("置顶失败，请重试").show()
                    }
                }
            }
        }
        addAction(getString(R.string.delete), R.drawable.ic_group_action_delete_outline, capabilities.canDelete) {
            confirmDashboardGroupDeletion(group, position)
        }
        if (content.childCount == 0) return
        val safe = android.graphics.Rect()
        anchor.getWindowVisibleDisplayFrame(safe)
        val root = anchor.rootView
        val rootLocation = IntArray(2)
        root.getLocationOnScreen(rootLocation)
        androidx.core.view.ViewCompat.getRootWindowInsets(root)?.getInsets(
            androidx.core.view.WindowInsetsCompat.Type.systemBars() or
                androidx.core.view.WindowInsetsCompat.Type.displayCutout()
        )?.let { insets ->
            safe.left = maxOf(safe.left, rootLocation[0] + insets.left)
            safe.top = maxOf(safe.top, rootLocation[1] + insets.top)
            safe.right = minOf(safe.right, rootLocation[0] + root.width - insets.right)
            safe.bottom = minOf(safe.bottom, rootLocation[1] + root.height - insets.bottom)
        }
        val location = IntArray(2)
        anchor.getLocationOnScreen(location)
        val margin = dp2px(8)
        val gap = dp2px(6)
        val availableWidth = (safe.width() - 2 * margin).coerceAtLeast(1)
        val availableHeight = (location[1] - gap - safe.top - margin).coerceAtLeast(1)
        content.measure(
            View.MeasureSpec.makeMeasureSpec(availableWidth, View.MeasureSpec.AT_MOST),
            View.MeasureSpec.makeMeasureSpec(0, View.MeasureSpec.UNSPECIFIED),
        )
        val popupWidth = content.measuredWidth.coerceIn(1, availableWidth)
        val popupHeight = content.measuredHeight.coerceIn(1, availableHeight)
        val scroll = android.widget.ScrollView(context).apply { addView(content) }
        window = PopupWindow(scroll, popupWidth, popupHeight, true).apply {
            isOutsideTouchable = true
            elevation = dp2px(10).toFloat()
            setBackgroundDrawable(android.graphics.drawable.ColorDrawable(Color.TRANSPARENT))
        }
        val minX = safe.left + margin
        val x = (location[0] + anchor.width / 2 - popupWidth / 2)
            .coerceIn(minX, (safe.right - margin - popupWidth).coerceAtLeast(minX))
        val y = (location[1] - popupHeight - gap).coerceAtLeast(safe.top + margin)
        window.showAtLocation(requireView(), Gravity.NO_GRAVITY, x, y)
    }

    private val pendingDashboardUpdates = mutableSetOf<Long>()

    private fun updateDashboardSubscription(group: ProxyGroup) {
        if (!canUpdateDashboardSubscription(group.type == GroupType.SUBSCRIPTION, group.subscription != null,
                group.id in GroupUpdater.updating || group.id in pendingDashboardUpdates)) {
            snackbar("订阅正在更新，请稍候").show()
            return
        }
        pendingDashboardUpdates.add(group.id)
        snackbar("正在更新订阅…").show()
        // Activity scope keeps the updater and its existing result dialogs alive across tab navigation.
        requireActivity().lifecycleScope.launch {
            try {
                withContext(Dispatchers.IO) { GroupUpdater.executeUpdate(group, true) }
            } finally {
                pendingDashboardUpdates.remove(group.id)
            }
        }
    }

    private fun confirmDashboardGroupDeletion(group: ProxyGroup, position: Int) {
        val ids = adapter.groupList.map { it.id }
        val fallbackId = fallbackGroupIdAfterDelete(ids, position) ?: return
        val capabilities = dashboardGroupMenuCapabilities(
            group.ungrouped,
            group.type == GroupType.SUBSCRIPTION,
            !group.subscription?.link.isNullOrBlank(),
            adapter.groupList.size,
            group.id in GroupUpdater.updating,
        )
        if (!capabilities.canDelete) return
        MaterialAlertDialogBuilder(requireContext())
            .setTitle(R.string.confirm)
            .setMessage(if (group.type == GroupType.PREFERRED)
                "仅删除优选容器及引用；来源分组和原节点保留，确定继续吗？"
                else "删除该分组会同时删除其中全部节点，确定继续吗？")
            .setPositiveButton(R.string.yes) { _, _ ->
                DataStore.selectedGroup = fallbackId
                runOnDefaultDispatcher {
                    GroupManager.deleteGroup(listOf(group))
                }
            }
            .setNegativeButton(android.R.string.cancel, null)
            .show()
    }

    override fun onQueryTextChange(query: String): Boolean {
        getCurrentGroupFragment()?.adapter?.filterGlobal(query)
        tabLayout.isGone = query.isNotBlank() || adapter.groupList.size < 2
        groupPager.isUserInputEnabled = query.isBlank()
        return true
    }

    override fun onQueryTextSubmit(query: String): Boolean = false

    @SuppressLint("DetachAndAttachSameFragment")
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setHasOptionsMenu(true)
        startProfileStateActor()
        refreshProfileState()

        if (savedInstanceState != null) {
            parentFragmentManager.beginTransaction()
                .setReorderingAllowed(false)
                .detach(this)
                .attach(this)
                .commit()
        }
    }

    override fun onViewCreated(view: View, savedInstanceState: Bundle?) {
        super.onViewCreated(view, savedInstanceState)

        if (!select) {
            toolbar.inflateMenu(R.menu.add_profile_menu)
            toolbar.menu.findItem(R.id.action_global_mode)?.isChecked = DataStore.globalMode
            toolbar.setOnMenuItemClickListener(this)
        } else {
            toolbar.setTitle(titleRes)
            toolbar.setNavigationIcon(R.drawable.ic_navigation_close)
            toolbar.setNavigationOnClickListener {
                requireActivity().finish()
            }
        }

        val searchView = toolbar.findViewById<SearchView>(R.id.action_search)
        if (searchView != null) {
            searchView.setOnQueryTextListener(this)
            searchView.maxWidth = Int.MAX_VALUE

            searchView.setOnQueryTextFocusChangeListener { _, hasFocus ->
                if (!hasFocus) {
                    cancelSearch(searchView)
                }
            }
        }

        dashboardConnectionButton = view.findViewById(R.id.dashboard_connection_button)
        dashboardConnectionIcon = view.findViewById(R.id.dashboard_connection_icon)
        dashboardState = view.findViewById(R.id.dashboard_connection_state)
        dashboardAction = view.findViewById(R.id.dashboard_connection_action)
        dashboardSessionUpload = view.findViewById(R.id.dashboard_session_upload)
        dashboardSessionDownload = view.findViewById(R.id.dashboard_session_download)
        dashboardTestCard = view.findViewById(R.id.dashboard_test_card)
        dashboardTestStatus = view.findViewById(R.id.dashboard_test_status)
        dashboardLatency = view.findViewById(R.id.dashboard_latency)
        dashboardUpload = view.findViewById(R.id.dashboard_upload)
        dashboardDownload = view.findViewById(R.id.dashboard_download)
        dashboardConnectionButton.setOnClickListener {
            when {
                DataStore.serviceState.canStop -> (activity as? MainActivity)?.stopDashboardConnection()
                !DataStore.serviceState.started -> (activity as? MainActivity)?.requestDashboardConnection()
            }
        }
        dashboardTestCard.setOnClickListener {
            (activity as? MainActivity)?.runDashboardConnectionTest()
        }
        updateDashboardState()

        groupPager = view.findViewById(R.id.group_pager)
        tabLayout = view.findViewById(R.id.group_tab)
        adapter = GroupPagerAdapter()
        ProfileManager.addListener(adapter)
        GroupManager.addListener(adapter)

        groupPager.adapter = adapter
        groupPager.offscreenPageLimit = 2

        TabLayoutMediator(tabLayout, groupPager) { tab, position ->
            if (adapter.groupList.size > position) {
                tab.text = adapter.groupList[position].displayName()
            }
            installGroupTabInteractions(tab, position)
        }.attach()

        // Group tab actions are anchored to the long-pressed tab.
        toolbar.setOnClickListener {
            val fragment = getCurrentGroupFragment()

            if (fragment != null) {
                val selectedProxy = selectedItem?.id ?: DataStore.selectedProxy
                val selectedProfileIndex =
                    fragment.adapter!!.configurationIdList.indexOf(selectedProxy)
                if (selectedProfileIndex != -1) {
                    val layoutManager = fragment.layoutManager
                    if (layoutManager is LinearLayoutManager) {
                        val first = layoutManager.findFirstVisibleItemPosition()
                        val last = layoutManager.findLastVisibleItemPosition()

                        if (selectedProfileIndex !in first..last) {
                            fragment.configurationListView.scrollTo(selectedProfileIndex, true)
                            return@setOnClickListener
                        }
                    } else {
                        fragment.configurationListView.scrollTo(selectedProfileIndex, true)
                        return@setOnClickListener
                    }

                }

                fragment.configurationListView.scrollTo(0)
            }

        }

        DataStore.profileCacheStore.registerChangeListener(this)
    }

    override fun onPrepareOptionsMenu(menu: Menu) {
        menu.findItem(R.id.action_global_mode)?.isChecked = DataStore.globalMode
        super.onPrepareOptionsMenu(menu)
    }

    override fun onPreferenceDataStoreChanged(store: PreferenceDataStore, key: String) {
        runOnMainDispatcher {
            // editingGroup
            if (key == Key.PROFILE_GROUP) {
                val targetId = DataStore.editingGroup
                if (targetId > 0 && targetId != DataStore.selectedGroup) {
                    DataStore.selectedGroup = targetId
                    val targetIndex = adapter.groupList.indexOfFirst { it.id == targetId }
                    if (targetIndex >= 0) {
                        groupPager.setCurrentItem(targetIndex, false)
                    } else {
                        adapter.reload()
                    }
                }
            }
        }
    }

    override fun onDestroyView() {
        stopDashboardPulse()
        speedTestDialog?.dismiss()
        speedTestDialog = null
        speedTestDialogProfileId = 0L
        speedTestDialogPhase = null
        speedTestDialogUpload = null
        speedTestDialogDownload = null
        speedTestDialogProgress = null
        super.onDestroyView()
    }

    override fun onDestroy() {
        DataStore.profileCacheStore.unregisterChangeListener(this)

        if (::adapter.isInitialized) {
            GroupManager.removeListener(adapter)
            ProfileManager.removeListener(adapter)
        }

        super.onDestroy()
    }

    override fun onKeyDown(ketCode: Int, event: KeyEvent): Boolean {
        val fragment = getCurrentGroupFragment()
        fragment?.configurationListView?.apply {
            if (!hasFocus()) requestFocus()
        }
        return super.onKeyDown(ketCode, event)
    }

    private val importFile =
        registerForActivityResult(ActivityResultContracts.GetContent()) { file ->
            if (file != null) runOnDefaultDispatcher {
                try {
                    val fileName =
                        requireContext().contentResolver.query(file, null, null, null, null)
                            ?.use { cursor ->
                                cursor.moveToFirst()
                                cursor.getColumnIndexOrThrow(OpenableColumns.DISPLAY_NAME)
                                    .let(cursor::getString)
                            }
                    val proxies = mutableListOf<AbstractBean>()
                    if (fileName != null && fileName.endsWith(".zip")) {
                        // try parse wireguard zip
                        val zip =
                            ZipInputStream(requireContext().contentResolver.openInputStream(file)!!)
                        while (true) {
                            val entry = zip.nextEntry ?: break
                            if (entry.isDirectory) continue
                            val fileText = zip.bufferedReader().readText()
                            RawUpdater.parseRaw(fileText, entry.name)
                                ?.let { pl -> proxies.addAll(pl) }
                            zip.closeEntry()
                        }
                        zip.closeQuietly()
                    } else {
                        val fileText =
                            requireContext().contentResolver.openInputStream(file)!!.use {
                                it.bufferedReader().readText()
                            }
                        RawUpdater.parseRaw(fileText, fileName ?: "")
                            ?.let { pl -> proxies.addAll(pl) }
                    }
                    if (proxies.isEmpty()) onMainDispatcher {
                        snackbar(getString(R.string.no_proxies_found_in_file)).show()
                    } else import(proxies)
                } catch (e: SubscriptionFoundException) {
                    (requireActivity() as MainActivity).importSubscription(e.link.toUri())
                } catch (e: Exception) {
                    Logs.w(e)
                    onMainDispatcher {
                        snackbar(e.readableMessage).show()
                    }
                }
            }
        }

    suspend fun import(proxies: List<AbstractBean>) {
        val targetId = DataStore.selectedGroupForImport()
        ProfileManager.createProfiles(targetId, proxies)

        onMainDispatcher {
            DataStore.editingGroup = targetId
            snackbar(
                requireContext().resources.getQuantityString(
                    R.plurals.added, proxies.size, proxies.size
                )
            ).show()
        }

    }

    private fun fetchAirportName(link: String): String? {
        if (!link.startsWith("http")) return null
        val client = Libcore.newHttpClient().apply {
            trySocks5(
                DataStore.mixedPort,
                DataStore.mixedInboundUser,
                DataStore.mixedInboundPass
            )
            tryH3Direct()
            when (DataStore.appTLSVersion) {
                "1.3" -> restrictedTLS()
            }
        }
        try {
            val response = client.newRequest().apply {
                if (DataStore.allowInsecureOnRequest) {
                    allowInsecure()
                }
                setURL(link)
                setUserAgent(USER_AGENT)
            }.execute()

            var remoteName = RawUpdater.parseBodyProfileTitle(Util.getStringBox(response.contentString))
            if (remoteName.isBlank()) {
                remoteName = parseContentDisposition(Util.getStringBox(response.getHeader("content-disposition")))
            }
            if (remoteName.isBlank()) {
                remoteName = decodeProfileTitle(Util.getStringBox(response.getHeader("profile-title")))
            }
            return remoteName.takeIf { it.isNotBlank() }
        } catch (e: Exception) {
            Logs.w(e)
            return null
        } finally {
            client.close()
        }
    }

    private fun parseContentDisposition(header: String): String {
        if (header.isBlank()) return ""
        val decoded = Util.decodeFilename(header).trim()
        if (decoded.isNotBlank()) return decoded
        return Regex("filename=\"?([^\";]+)\"?").find(header)?.groupValues?.get(1)?.trim() ?: ""
    }

    private fun decodeProfileTitle(header: String): String {
        if (header.isBlank()) return ""
        val title = header.trim()
        return try {
            URLDecoder.decode(title, "UTF-8")
        } catch (e: Exception) {
            title
        }
    }

    override fun onMenuItemClick(item: MenuItem): Boolean {
        when (item.itemId) {
            R.id.action_scan_qr_code -> {
                startActivity(Intent(context, ScannerActivity::class.java))
            }

            R.id.action_import_clipboard -> {
                val text = SagerNet.getClipboardText()
                if (text.isBlank()) {
                    snackbar(getString(R.string.clipboard_empty)).show()
                } else runOnDefaultDispatcher {
                    try {
                        val proxies = RawUpdater.parseRaw(text)
                        if (proxies.isNullOrEmpty()) {
                            onMainDispatcher {
                                snackbar(getString(R.string.no_proxies_found_in_clipboard)).show()
                            }
                        } else {
                            import(proxies)
                        }
                    } catch (e: SubscriptionFoundException) {
                        if (e.link.startsWith("sn://")) {
                            onMainDispatcher {
                                (requireActivity() as MainActivity).importSubscription(e.link.toUri())
                            }
                        } else {
                            val subscriptionUri = Uri.parse(e.link)
                            val subscriptionLink = subscriptionUri.getQueryParameter("url") ?: e.link
                            val oppaProviderName = if (subscriptionLink.startsWith("oppa://")) {
                                runCatching { parseOppaProvider(subscriptionLink).name }.getOrNull()
                            } else null
                            val airportName = oppaProviderName
                                ?: subscriptionUri.getQueryParameter("name")?.takeIf { it.isNotBlank() }

                            val group = ProxyGroup(type = GroupType.SUBSCRIPTION)
                            val subscription = SubscriptionBean()
                            group.subscription = subscription
                            subscription.link = subscriptionLink
                            subscription.autoUpdate = false
                            group.name = airportName ?: ""
                            onMainDispatcher {
                                startActivity(Intent(requireContext(), GroupSettingsActivity::class.java).apply {
                                    putExtra(GroupSettingsActivity.EXTRA_FROM_CLIPBOARD, true)
                                    putExtra(GroupSettingsActivity.EXTRA_GROUP_SUBSCRIPTION_LINK, subscriptionLink)
                                    if (airportName != null) {
                                        putExtra(GroupSettingsActivity.EXTRA_GROUP_NAME, airportName)
                                    }
                                })
                            }
                        }
                    } catch (e: Exception) {
                        Logs.w(e)
                        onMainDispatcher {
                            snackbar(e.readableMessage).show()
                        }
                    }
                }
            }

            R.id.action_import_file -> {
                startFilesForResult(importFile, "*/*")
            }

            R.id.action_new_preferred -> {
                startActivity(Intent(requireActivity(), PreferredGroupActivity::class.java))
            }

            R.id.action_new_socks -> {
                startActivity(Intent(requireActivity(), SocksSettingsActivity::class.java))
            }

            R.id.action_new_http -> {
                startActivity(Intent(requireActivity(), HttpSettingsActivity::class.java))
            }

            R.id.action_new_ss -> {
                startActivity(Intent(requireActivity(), ShadowsocksSettingsActivity::class.java))
            }

            R.id.action_new_ssr -> {
                startActivity(Intent(requireActivity(), ShadowsocksRSettingsActivity::class.java))
            }

            R.id.action_new_vmess -> {
                startActivity(Intent(requireActivity(), VMessSettingsActivity::class.java))
            }

            R.id.action_new_vless -> {
                startActivity(Intent(requireActivity(), VMessSettingsActivity::class.java).apply {
                    putExtra("vless", true)
                })
            }

            R.id.action_new_trojan -> {
                startActivity(Intent(requireActivity(), TrojanSettingsActivity::class.java))
            }

            R.id.action_new_trojan_go -> {
                startActivity(Intent(requireActivity(), TrojanGoSettingsActivity::class.java))
            }

            R.id.action_new_mieru -> {
                startActivity(Intent(requireActivity(), MieruSettingsActivity::class.java))
            }

            R.id.action_new_naive -> {
                startActivity(Intent(requireActivity(), NaiveSettingsActivity::class.java))
            }

            R.id.action_new_hysteria -> {
                startActivity(Intent(requireActivity(), HysteriaSettingsActivity::class.java))
            }

            R.id.action_new_tuic -> {
                startActivity(Intent(requireActivity(), TuicSettingsActivity::class.java))
            }

            R.id.action_new_juicity -> {
                startActivity(Intent(requireActivity(), JuicitySettingsActivity::class.java))
            }

            R.id.action_new_oppa -> {
                startActivity(Intent(requireActivity(), OppaSettingsActivity::class.java))
            }

            R.id.action_new_ssh -> {
                startActivity(Intent(requireActivity(), SSHSettingsActivity::class.java))
            }

            R.id.action_new_snell -> {
                startActivity(Intent(requireActivity(), SnellSettingsActivity::class.java))
            }

            R.id.action_new_wg -> {
                startActivity(Intent(requireActivity(), WireGuardSettingsActivity::class.java))
            }

            R.id.action_new_shadowtls -> {
                startActivity(Intent(requireActivity(), ShadowTLSSettingsActivity::class.java))
            }

            R.id.action_new_anytls -> {
                startActivity(Intent(requireActivity(), AnyTLSSettingsActivity::class.java))
            }

            R.id.action_new_config -> {
                startActivity(Intent(requireActivity(), ConfigSettingActivity::class.java))
            }

            R.id.action_new_chain -> {
                startActivity(Intent(requireActivity(), ChainSettingsActivity::class.java))
            }

            R.id.action_update_subscription -> {
                val group = DataStore.currentGroup()
                if (group.type != GroupType.SUBSCRIPTION) {
                    snackbar(R.string.group_not_subscription).show()
                    Logs.e("onMenuItemClick: Group(${group.displayName()}) is not subscription")
                } else {
                    runOnLifecycleDispatcher {
                        GroupUpdater.startUpdate(group, true)
                    }
                }
            }

            R.id.action_clear_traffic_statistics -> {
                val trafficService = (activity as? MainActivity)?.connection?.service
                runOnDefaultDispatcher {
                    val profiles = SagerDatabase.proxyDao.getByGroup(DataStore.currentGroupId())
                    val toClear = mutableListOf<ProxyEntity>()
                    if (profiles.isNotEmpty()) for (profile in profiles) {
                        if (profile.tx != 0L || profile.rx != 0L) {
                            profile.tx = 0
                            profile.rx = 0
                            toClear.add(profile)
                        }
                    }
                    if (toClear.isNotEmpty()) {
                        ProfileManager.updateProfile(toClear)
                    }
                    try {
                        trafficService?.resetTraffic(profiles.map { it.id }.toLongArray())
                    } catch (e: Exception) {
                        Logs.w(e)
                    }
                    onMainDispatcher {
                        getCurrentGroupFragment()?.adapter?.clearTrafficStatistics()
                    }
                }
            }

            R.id.action_connection_test_clear_results -> {
                runOnDefaultDispatcher {
                    val profiles = SagerDatabase.proxyDao.getByGroup(DataStore.currentGroupId())
                    val toClear = mutableListOf<ProxyEntity>()
                    if (profiles.isNotEmpty()) for (profile in profiles) {
                        if (profile.status != 0) {
                            profile.status = 0
                            profile.ping = 0
                            profile.error = null
                            toClear.add(profile)
                        }
                    }
                    if (toClear.isNotEmpty()) {
                        ProfileManager.updateProfile(toClear)
                    }
                    onMainDispatcher {
                        getCurrentGroupFragment()?.adapter?.clearTestResults()
                    }
                }
            }

            R.id.action_connection_test_delete_unavailable -> {
                runOnDefaultDispatcher {
                    val profiles = SagerDatabase.proxyDao.getByGroup(DataStore.currentGroupId())
                    val toClear = mutableListOf<ProxyEntity>()
                    if (profiles.isNotEmpty()) for (profile in profiles) {
                        if (profile.status != 0 && profile.status != 1) {
                            toClear.add(profile)
                        }
                    }
                    if (toClear.isNotEmpty()) {
                        onMainDispatcher {
                            MaterialAlertDialogBuilder(requireContext()).setTitle(R.string.confirm)
                                .setMessage(R.string.delete_confirm_prompt)
                                .setPositiveButton(R.string.yes) { _, _ ->
                                    for (profile in toClear) {
                                        adapter.groupFragments[DataStore.selectedGroup]?.adapter?.apply {
                                            val index = configurationIdList.indexOf(profile.id)
                                            if (index >= 0) {
                                                configurationIdList.removeAt(index)
                                                configurationList.remove(profile.id)
                                                notifyItemRemoved(index)
                                            }
                                        }
                                    }
                                    runOnDefaultDispatcher {
                                        for (profile in toClear) {
                                            ProfileManager.deleteProfile2(
                                                profile.groupId, profile.id
                                            )
                                        }
                                    }
                                }
                                .setNegativeButton(R.string.no, null)
                                .show()
                        }
                    }
                }
            }

            R.id.action_remove_duplicate -> {
                runOnDefaultDispatcher {
                    val profiles = SagerDatabase.proxyDao.getByGroup(DataStore.currentGroupId())
                    val toClear = mutableListOf<ProxyEntity>()
                    val uniqueProxies = LinkedHashSet<Protocols.Deduplication>()
                    for (pf in profiles) {
                        val proxy = Protocols.Deduplication(pf.requireBean(), pf.displayType())
                        if (!uniqueProxies.add(proxy)) {
                            toClear += pf
                        }
                    }
                    if (toClear.isNotEmpty()) {
                        onMainDispatcher {
                            MaterialAlertDialogBuilder(requireContext()).setTitle(R.string.confirm)
                                .setMessage(
                                    getString(R.string.delete_confirm_prompt) + "\n" +
                                            toClear.mapIndexedNotNull { index, proxyEntity ->
                                                if (index < 20) {
                                                    proxyEntity.displayName()
                                                } else if (index == 20) {
                                                    "......"
                                                } else {
                                                    null
                                                }
                                            }.joinToString("\n")
                                )
                                .setPositiveButton(R.string.yes) { _, _ ->
                                    for (profile in toClear) {
                                        adapter.groupFragments[DataStore.selectedGroup]?.adapter?.apply {
                                            val index = configurationIdList.indexOf(profile.id)
                                            if (index >= 0) {
                                                configurationIdList.removeAt(index)
                                                configurationList.remove(profile.id)
                                                notifyItemRemoved(index)
                                            }
                                        }
                                    }
                                    runOnDefaultDispatcher {
                                        for (profile in toClear) {
                                            ProfileManager.deleteProfile2(
                                                profile.groupId, profile.id
                                            )
                                        }
                                    }
                                }
                                .setNegativeButton(R.string.no, null)
                                .show()
                        }
                    }
                }
            }

            R.id.action_connection_tcp_ping -> {
                pingTest(false)
            }

            R.id.action_connection_url_test -> {
                MaterialAlertDialogBuilder(requireContext())
                    .setTitle(R.string.connection_test_url_test)
                    .setItems(R.array.url_test_scopes) { _, which ->
                        urlTest(io.nekohasekai.sagernet.bg.proto.UrlTestScope.values()[which])
                    }.show()
            }

            R.id.action_global_mode -> {
                item.isChecked = !item.isChecked
                DataStore.globalMode = item.isChecked
                if (DataStore.serviceState.canStop) {
                    runOnDefaultDispatcher {
                        try {
                            // 等待一段时间确保配置已保存
                            delay(500)
                            snackbar(getString(R.string.need_reload)).setAction(R.string.apply) {
                                runOnDefaultDispatcher {
                                    try {
                                        // 再次等待确保配置已保存
                                        delay(100)
                                        SagerNet.reloadService()
                                    } catch (e: Exception) {
                                        Logs.w(e)
                                        onMainDispatcher {
                                            snackbar(getString(R.string.service_failed)).show()
                                        }
                                    }
                                }
                            }.show()
                        } catch (e: Exception) {
                            Logs.w(e)
                            onMainDispatcher {
                                snackbar(getString(R.string.service_failed)).show()
                            }
                        }
                    }
                }
                return true
            }
        }
        return false
    }

    /** Preferred tests resolve source IDs, not the managed urltest container. */
    private fun testMembers(group: ProxyGroup): List<ProxyEntity> =
        if (group.type == GroupType.PREFERRED) PreferredGroupStore.rows(group.id).second.mapNotNull { it.first }.distinctBy { it.id }
        else SagerDatabase.proxyDao.getByGroup(group.id)

    /** Never write the stale full source snapshot back after a long-running test. */
    private suspend fun persistTestResult(result: ProxyEntity) {
        val updated = SagerDatabase.instance.runInTransaction(java.util.concurrent.Callable {
            SagerDatabase.proxyDao.getById(result.id)?.also {
                it.status = result.status
                it.ping = result.ping
                it.error = result.error
                SagerDatabase.proxyDao.updateProxy(it)
            }
        })
        updated?.let { ProfileManager.postUpdate(it) }
    }

    inner class TestDialog {
        val binding = LayoutProgressListBinding.inflate(layoutInflater)
        val builder = MaterialAlertDialogBuilder(requireContext()).setView(binding.root)
            .setPositiveButton(R.string.minimize) { _, _ ->
                minimize()
            }
            .setNegativeButton(android.R.string.cancel) { _, _ ->
                cancel()
            }
            .setCancelable(false)

        lateinit var cancel: () -> Unit
        lateinit var minimize: () -> Unit

        val dialogStatus = AtomicInteger(0) // 1: hidden 2: cancelled
        var notification: ConnectionTestNotification? = null

        var kind = "URLTest"
        private val tickets = ConcurrentHashMap<Long, Long>()
        fun finishPending() = preferredTestResults.finishPending(tickets)
        fun begin(profiles: List<ProxyEntity>) {
            profiles.forEach { tickets[it.id] = preferredTestResults.begin(it.id, kind) }
        }
        val results: MutableSet<ProxyEntity> = ConcurrentHashMap.newKeySet()
        var proxyN = 0
        val finishedN = AtomicInteger(0)

        fun update(profile: ProxyEntity) {
            if (dialogStatus.get() != 2) {
                results.add(profile)
                tickets[profile.id]?.let { preferredTestResults.complete(profile.id, it, profile.status, profile.ping) }
            }
            runOnMainDispatcher {
                val context = context ?: return@runOnMainDispatcher
                val progress = finishedN.addAndGet(1)
                val status = dialogStatus.get()
                notification?.updateNotification(
                    progress,
                    proxyN,
                    progress >= proxyN || status == 2
                )
                if (status >= 1) return@runOnMainDispatcher
                if (!isAdded) return@runOnMainDispatcher

                // refresh dialog

                var profileStatusText: String? = null
                var profileStatusColor = 0

                when (profile.status) {
                    -1 -> {
                        profileStatusText = profile.error
                        profileStatusColor = context.getColorAttr(android.R.attr.textColorSecondary)
                    }

                    0 -> {
                        profileStatusText = getString(R.string.connection_test_testing)
                        profileStatusColor = context.getColorAttr(android.R.attr.textColorSecondary)
                    }

                    1 -> {
                        profileStatusText = getString(R.string.available, profile.ping)
                        profileStatusColor = context.getColour(R.color.material_green_500)
                    }

                    2 -> {
                        profileStatusText = profile.error
                        profileStatusColor = context.getColour(R.color.material_red_500)
                    }

                    3 -> {
                        val err = profile.error ?: ""
                        val msg = Protocols.genFriendlyMsg(err)
                        profileStatusText = if (msg != err) msg else getString(R.string.unavailable)
                        profileStatusColor = context.getColour(R.color.material_red_500)
                    }
                }

                val text = SpannableStringBuilder().apply {
                    append("\n" + profile.displayName())
                    append("\n")
                    append(
                        profile.displayType(),
                        ForegroundColorSpan(context.getProtocolColor(profile.type)),
                        SPAN_EXCLUSIVE_EXCLUSIVE
                    )
                    append(" ")
                    append(
                        profileStatusText,
                        ForegroundColorSpan(profileStatusColor),
                        SPAN_EXCLUSIVE_EXCLUSIVE
                    )
                    append("\n")
                }

                binding.nowTesting.text = text
                binding.progress.text = "$progress / $proxyN"
            }
        }

    }

    @OptIn(DelicateCoroutinesApi::class)
    @Suppress("EXPERIMENTAL_API_USAGE")
    fun pingTest(icmpPing: Boolean) {
        if (DataStore.runningTest) return else DataStore.runningTest = true
        val test = TestDialog()
        val dialog = test.builder.show()
        test.kind = if (icmpPing) "ICMPing" else "TCPing"
        val testJobs = mutableListOf<Job>()
        val group = DataStore.currentGroup()

        val mainJob = runOnDefaultDispatcher {
            val profilesList = testMembers(group).filter {
                if (icmpPing) {
                    if (it.requireBean().canICMPing()) {
                        return@filter true
                    }
                } else {
                    if (it.requireBean().canTCPing()) {
                        return@filter true
                    }
                }
                return@filter false
            }
            test.begin(profilesList)
            val tunNetSelections = profilesList.associateWith { profile ->
                runCatching {
                    buildConfig(profile, forTest = true).trafficMap.values.flatten()
                        .mapNotNull { (it.requireBean() as? VMessBean)?.tunNetSelection() }
                        .distinct()
                        .singleOrNull()
                }.getOrNull()
            }
            test.proxyN = profilesList.size
            val tunNetPingBatch = if (tunNetSelections.values.any { it != null }) {
                withContext(Dispatchers.IO) {
                    Libcore.prepareTunNetBatch(
                        "https://client-api.nexttun.net/api/v1/client",
                        SagerNet.application.noBackupFilesDir.absolutePath,
                        TUN_NET_CLIENT_VERSION,
                    )
                }
            } else null
            val profiles = ConcurrentLinkedQueue(profilesList)
            try {
                repeat(DataStore.connectionTestConcurrent) {
                    testJobs.add(launch(Dispatchers.IO) {
                        while (isActive) {
                        val profile = profiles.poll() ?: break

                        profile.status = 0
                        val bean = profile.requireBean()
                        val tunNetSelection = tunNetSelections[profile]
                        var address = bean.serverAddress
                        var port = bean.serverPort
                        if (tunNetSelection != null && tunNetPingBatch != null) {
                            try {
                                val ingress = org.json.JSONObject(
                                    Libcore.resolveTunNetBatchIngress(tunNetPingBatch, tunNetSelection.entryNode)
                                )
                                address = ingress.getString("address")
                                port = ingress.optInt("port", 443)
                            } catch (e: Exception) {
                                profile.status = 3
                                profile.error = e.readableMessage
                                test.update(profile)
                                continue
                            }
                        }
                        if (!address.isIpAddress()) {
                            val customDns = SagerDatabase.groupDao.getById(profile.groupId)?.customDirectDns.takeIf { !it.isNullOrBlank() }
                                ?: DataStore.directDns.split("\n").firstOrNull { it.isNotBlank() && !it.startsWith("#") }
                            val resolvedIp = resolveDomainCustom(address, customDns)
                            if (resolvedIp != null) {
                                address = resolvedIp
                            }
                        }

                        if (!isActive) break
                        if (!address.isIpAddress()) {
                            profile.status = 2
                            profile.error = app.getString(R.string.connection_test_domain_not_found)
                            test.update(profile)
                            continue
                        }
                        try {
                            if (icmpPing) {
                                // removed
                            } else {
                                val socket =
                                    SagerNet.underlyingNetwork?.socketFactory?.createSocket()
                                        ?: Socket()
                                try {
                                    socket.soTimeout = 3000
                                    socket.bind(InetSocketAddress(0))
                                    val start = SystemClock.elapsedRealtime()
                                    socket.connect(
                                        InetSocketAddress(
                                            address, port
                                        ), 3000
                                    )
                                    if (!isActive) break
                                    profile.status = 1
                                    profile.ping = (SystemClock.elapsedRealtime() - start).toInt()
                                    test.update(profile)
                                } finally {
                                    socket.closeQuietly()
                                }
                            }
                        } catch (e: Exception) {
                            if (!isActive) break
                            val message = e.readableMessage

                            if (icmpPing) {
                                profile.status = 2
                                profile.error = getString(R.string.connection_test_unreachable)
                            } else {
                                profile.status = 2
                                when {
                                    !message.contains("failed:") -> profile.error =
                                        getString(R.string.connection_test_timeout_error)

                                    else -> when {
                                        message.contains("ECONNREFUSED") -> {
                                            profile.error =
                                                getString(R.string.connection_test_refused)
                                        }

                                        message.contains("ENETUNREACH") -> {
                                            profile.error =
                                                getString(R.string.connection_test_unreachable)
                                        }

                                        else -> {
                                            profile.status = 3
                                            profile.error = message
                                        }
                                    }
                                }
                            }
                            test.update(profile)
                        }
                    }
                })
            }

                testJobs.joinAll()
            } finally {
                if (tunNetPingBatch != null) {
                    runCatching { Libcore.releaseTunNetBatch(tunNetPingBatch) }
                }
            }

            runOnMainDispatcher {
                test.cancel()
            }
        }
        test.cancel = {
            test.dialogStatus.set(2)
            dialog.dismiss()
            runOnDefaultDispatcher {
                mainJob.cancel()
                testJobs.forEach { it.cancelAndJoin() }
                test.finishPending()
                test.results.forEach {
                    try {
                        persistTestResult(it)
                    } catch (e: Exception) {
                        Logs.w(e)
                    }
                }
                GroupManager.postReload(DataStore.currentGroupId())
                DataStore.runningTest = false
            }
        }
        test.minimize = {
            test.dialogStatus.set(1)
            test.notification = ConnectionTestNotification(
                dialog.context,
                "[${group.displayName()}] ${getString(R.string.connection_test)}"
            )
            dialog.hide()
        }
    }

    @OptIn(DelicateCoroutinesApi::class)
    fun urlTest(scope: io.nekohasekai.sagernet.bg.proto.UrlTestScope = io.nekohasekai.sagernet.bg.proto.UrlTestScope.ALL) {
        if (DataStore.runningTest) return
        val test = TestDialog()
        val dialog = test.builder.show()
        val group = DataStore.currentGroup()
        DataStore.runningTest = true
        val mainJob = kotlinx.coroutines.GlobalScope.launch(
            Dispatchers.Default, start = kotlinx.coroutines.CoroutineStart.ATOMIC,
        ) {
            val started = System.nanoTime()
            val batchDirectory = File(SagerNet.application.noBackupFilesDir, "tunnet/tests/${UUID.randomUUID()}")
            var tunNetBatch: String? = null
            try {
                val profilesList = testMembers(group).filter { scope.includes(it.status) }
                test.begin(profilesList)
                test.proxyN = profilesList.size
                val tunNetSelections = profilesList.associateWith { profile ->
                    ensureActive()
                    io.nekohasekai.sagernet.bg.proto.discoverUrlTestTunNet(profile,
                        SagerDatabase.groupDao.getById(profile.groupId) ?: group) {
                        runCatching {
                            buildConfig(profile, forTest = true).trafficMap.values.flatten()
                                .mapNotNull { (it.requireBean() as? VMessBean)?.tunNetSelection() }
                                .distinct().singleOrNull()
                        }.getOrNull()
                    }
                }
                if (tunNetSelections.values.any { it != null }) {
                    // Assign inside the IO block: prompt cancellation must not lose the native handle.
                    withContext(Dispatchers.IO) {
                        tunNetBatch = Libcore.prepareTunNetBatch(
                            "https://client-api.nexttun.net/api/v1/client",
                            SagerNet.application.noBackupFilesDir.absolutePath,
                            TUN_NET_CLIENT_VERSION,
                        )
                    }
                }
                Logs.i("URLTest batch count=${profilesList.size} workers=${io.nekohasekai.sagernet.bg.proto.urlTestWorkerCount(DataStore.connectionTestConcurrent, profilesList.size)} discoveryMs=${(System.nanoTime()-started)/1_000_000}")
                val urlTest = UrlTest()
                io.nekohasekai.sagernet.bg.proto.runUrlTestBatch(profilesList, DataStore.connectionTestConcurrent) { profile ->
                    // No mutation until a fresh result exists. Cancellation leaves history untouched.
                    var status: Int
                    var ping = 0
                    var error: String? = null
                    try {
                        val selection = tunNetSelections[profile]
                        val snapshotPath = if (selection != null && tunNetBatch != null) {
                            File(batchDirectory, "${profile.id}-${UUID.randomUUID()}/snapshot.json").absolutePath
                        } else null
                        ping = urlTest.doTest(profile, tunNetBatch.takeIf { selection != null }, snapshotPath)
                        status = 1
                    } catch (e: CancellationException) {
                        throw e
                    } catch (e: PluginManager.PluginNotFoundException) {
                        status = 2
                        error = e.readableMessage
                    } catch (e: Exception) {
                        status = 3
                        error = e.readableMessage
                    }
                    ensureActive()
                    synchronized(test.results) {
                        if (test.dialogStatus.get() != 2) {
                            profile.status = status
                            profile.ping = ping
                            profile.error = error
                            test.update(profile)
                        }
                    }
                }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                Logs.w("URLTest batch preparation failed: ${e.javaClass.simpleName}")
                withContext(Dispatchers.Main) { if (isAdded) snackbar(e.readableMessage).show() }
            } finally {
                withContext(NonCancellable) {
                    test.finishPending()
                    tunNetBatch?.let { handle -> runCatching { Libcore.releaseTunNetBatch(handle) } }
                    batchDirectory.deleteRecursively()
                    test.results.forEach {
                        try { persistTestResult(it) } catch (e: Exception) { Logs.w(e) }
                    }
                    try { GroupManager.postReload(group.id) } finally {
                        withContext(Dispatchers.Main) {
                            test.notification?.updateNotification(test.finishedN.get(), test.proxyN, true)
                            dialog.dismiss()
                            DataStore.runningTest = false
                        }
                    }
                    Logs.i("URLTest batch completed=${test.results.size} totalMs=${(System.nanoTime()-started)/1_000_000}")
                }
            }
        }
        test.cancel = {
            synchronized(test.results) { test.dialogStatus.set(2) }
            dialog.dismiss()
            mainJob.cancel()
            // runningTest stays true until all worker requests and cleanup have actually drained.
        }
        test.minimize = {
            test.dialogStatus.set(1)
            test.notification = ConnectionTestNotification(
                dialog.context,
                "[${group.displayName()}] ${getString(R.string.connection_test)}"
            )
            dialog.hide()
        }
    }

    inner class GroupPagerAdapter : FragmentStateAdapter(this),
        ProfileManager.Listener,
        GroupManager.Listener {

        var selectedGroupIndex = 0
        var groupList: ArrayList<ProxyGroup> = ArrayList()
        var groupFragments: HashMap<Long, GroupFragment> = HashMap()
        var preferredGroupFragments: HashMap<Long, PreferredGroupFragment> = HashMap()
        private val reloadGeneration = AtomicLong()

        fun reload(now: Boolean = false) {
            val generation = reloadGeneration.incrementAndGet()
            val visibleGroupId = groupList.getOrNull(selectedGroupIndex)?.id
                ?: DataStore.currentGroupId()

            if (!select) {
                groupPager.unregisterOnPageChangeCallback(updateSelectedCallback)
            }

            runOnDefaultDispatcher {
                PreferredGroupStore.migrateLegacy()
                var newGroupList = ArrayList(SagerDatabase.groupDao.allGroups())
                if (newGroupList.isEmpty()) {
                    SagerDatabase.groupDao.createGroup(ProxyGroup(ungrouped = true))
                    newGroupList = ArrayList(SagerDatabase.groupDao.allGroups())
                }
                newGroupList.find { it.ungrouped }?.let {
                    if (SagerDatabase.proxyDao.countByGroup(it.id) == 0L) {
                        newGroupList.remove(it)
                    }
                }

                if (generation != reloadGeneration.get()) return@runOnDefaultDispatcher

                var selectedGroup = visibleGroupId
                var newSelectedGroupIndex: Int? = null
                if (selectedGroup > 0L) {
                    newSelectedGroupIndex = newGroupList.indexOfFirst { it.id == selectedGroup }
                        .takeIf { it >= 0 }
                }
                if (newSelectedGroupIndex == null && newGroupList.isNotEmpty()) {
                    selectedGroup = newGroupList.first().id
                    newSelectedGroupIndex = 0
                    if (DataStore.selectedGroup != selectedGroup) {
                        DataStore.selectedGroup = selectedGroup
                    }
                }

                val runFunc = if (now) activity?.let { it::runOnUiThread } else groupPager::post
                if (runFunc != null) {
                    val reloadAdapter = this@GroupPagerAdapter
                    runFunc {
                        val viewOwner = viewLifecycleOwnerLiveData.value
                        if (generation == reloadGeneration.get() && viewOwner != null &&
                            isCurrentGroupPagerAdapter(reloadAdapter)
                        ) {
                            viewOwner.lifecycleScope.launch(Dispatchers.Main.immediate) {
                                profileStateInitialized.await()
                                if (generation != reloadGeneration.get() ||
                                    viewLifecycleOwnerLiveData.value !== viewOwner ||
                                    !isCurrentGroupPagerAdapter(reloadAdapter)
                                ) {
                                    return@launch
                                }
                                refreshProfileState()
                                newSelectedGroupIndex?.let { selectedGroupIndex = it }
                                groupList = newGroupList
                                notifyDataSetChanged()
                                if (newSelectedGroupIndex != null) {
                                    restoreGroupTabPosition(selectedGroupIndex)
                                }
                                val hideTab = groupList.size < 2
                                tabLayout.isGone = hideTab
                                toolbar.elevation = if (hideTab) 0F else dp2px(4).toFloat()
                                if (!select) {
                                    groupPager.registerOnPageChangeCallback(updateSelectedCallback)
                                }
                            }
                        }
                    }
                }
            }
        }

        init {
            reload(true)
        }

        override fun getItemCount(): Int {
            return groupList.size
        }

        override fun createFragment(position: Int): Fragment {
            if (groupList[position].type == GroupType.PREFERRED) {
                val fragment = PreferredGroupFragment.newInstance(groupList[position].id, select)
                preferredGroupFragments[groupList[position].id] = fragment
                return fragment
            }
            return GroupFragment().apply {
                proxyGroup = groupList[position]
                groupFragments[proxyGroup.id] = this
                if (position == selectedGroupIndex) {
                    selected = true
                }
            }
        }

        override fun getItemId(position: Int): Long {
            return groupList[position].id
        }

        override fun containsItem(itemId: Long): Boolean {
            return groupList.any { it.id == itemId }
        }

        override suspend fun groupAdd(group: ProxyGroup) {
            tabLayout.post {
                groupList.add(group)
                selectedGroupIndex = groupList.lastIndex
                DataStore.selectedGroup = group.id
                tabLayout.visibility = View.VISIBLE
                notifyItemInserted(groupList.size - 1)
                tabLayout.post { restoreGroupTabPosition(groupList.lastIndex) }
            }
        }

        override suspend fun groupRemoved(groupId: Long) {
            val index = groupList.indexOfFirst { it.id == groupId }
            if (index == -1) return

            tabLayout.post {
                val fallbackId = if (DataStore.selectedGroup == groupId) {
                    fallbackGroupIdAfterDelete(groupList.map { it.id }, index)
                } else null
                groupList.removeAt(index)
                groupFragments.remove(groupId)
                preferredGroupFragments.remove(groupId)
                fallbackId?.let { DataStore.selectedGroup = it }
                if (DataStore.selectedGroup == groupId) {
                    selectedGroupIndex = groupList.indexOfFirst { it.id == DataStore.selectedGroup }
                        .takeIf { it >= 0 } ?: 0
                }
                notifyItemRemoved(index)
                if (groupList.isNotEmpty()) {
                    restoreGroupTabPosition(selectedGroupIndex.coerceIn(groupList.indices))
                }
                tabLayout.isGone = groupList.size < 2
            }
        }

        override suspend fun groupUpdated(group: ProxyGroup) {
            val index = groupList.indexOfFirst { it.id == group.id }
            if (index == -1) return

            tabLayout.post {
                groupList[index] = group
                tabLayout.getTabAt(index)?.text = group.displayName()
            }
        }

        override suspend fun groupUpdated(groupId: Long) { onMainDispatcher { reload() } }

        override suspend fun onAdd(profile: ProxyEntity) {
            if (groupList.find { it.id == profile.groupId } == null) {
                DataStore.selectedGroup = profile.groupId
                reload()
            }
        }

        override suspend fun onBatchAdded(profiles: List<ProxyEntity>) {
            val groupId = profiles.firstOrNull()?.groupId ?: return
            if (groupList.none { it.id == groupId }) {
                DataStore.selectedGroup = groupId
                onMainDispatcher { reload(now = true) }
            }
        }

        override suspend fun onUpdated(data: List<TrafficData>) = Unit

        override suspend fun onUpdated(profile: ProxyEntity, noTraffic: Boolean) = Unit

        override suspend fun onRemoved(groupId: Long, profileId: Long) {
            val group = groupList.find { it.id == groupId } ?: return
            if (group.ungrouped && SagerDatabase.proxyDao.countByGroup(groupId) == 0L) {
                reload()
            }
        }
    }

    class GroupFragment : Fragment() {

        lateinit var proxyGroup: ProxyGroup
        var selected = false

        override fun onCreateView(
            inflater: LayoutInflater,
            container: ViewGroup?,
            savedInstanceState: Bundle?,
        ): View {
            return LayoutProfileListBinding.inflate(inflater).root
        }

        lateinit var undoManager: UndoSnackbarManager<ProxyEntity>
        var adapter: ConfigurationAdapter? = null

        override fun onSaveInstanceState(outState: Bundle) {
            super.onSaveInstanceState(outState)

            if (::proxyGroup.isInitialized) {
                outState.putParcelable("proxyGroup", proxyGroup)
            }
        }

        override fun onViewStateRestored(savedInstanceState: Bundle?) {
            super.onViewStateRestored(savedInstanceState)

            savedInstanceState?.getParcelable<ProxyGroup>("proxyGroup")?.also {
                proxyGroup = it
                onViewCreated(requireView(), null)
            }
        }

        private val isEnabled: Boolean
            get() {
                return DataStore.serviceState.let { it.canStop || it == BaseService.State.Stopped }
            }

        lateinit var layoutManager: RecyclerView.LayoutManager
        private lateinit var itemTouchHelper: ItemTouchHelper
        private val alwaysShowAddress: Boolean
            get() = (parentFragment as? ConfigurationFragment)?.alwaysShowAddress == true

        private fun armManualDrag(view: View, holder: RecyclerView.ViewHolder) {
            if (select) {
                view.setOnLongClickListener(null)
                return
            }
            view.setOnLongClickListener {
                if (::itemTouchHelper.isInitialized && isEnabled && adapter?.isGlobalSearch != true) {
                    itemTouchHelper.startDrag(holder)
                    true
                } else false
            }
        }

        private fun setupItemTouchHelper() {
            if (select) return
            
            if (::itemTouchHelper.isInitialized) {
                itemTouchHelper.attachToRecyclerView(null)
            }
            
            itemTouchHelper = ItemTouchHelper(object : ItemTouchHelper.SimpleCallback(0, 0) {
                override fun isLongPressDragEnabled(): Boolean = false

                override fun getMovementFlags(
                    recyclerView: RecyclerView,
                    viewHolder: RecyclerView.ViewHolder
                ): Int {
                    if (adapter?.isGlobalSearch == true) return makeMovementFlags(0, 0)
                    val dragFlags = if (DataStore.groupLayoutMode == 1) {
                        ItemTouchHelper.UP or ItemTouchHelper.DOWN or ItemTouchHelper.LEFT or ItemTouchHelper.RIGHT
                    } else {
                        ItemTouchHelper.UP or ItemTouchHelper.DOWN
                    }
                    return makeMovementFlags(dragFlags, 0) // No swipe flags
                }

                override fun getSwipeDirs(
                    recyclerView: RecyclerView,
                    viewHolder: RecyclerView.ViewHolder,
                ): Int {
                    return 0
                }

                override fun getDragDirs(
                    recyclerView: RecyclerView,
                    viewHolder: RecyclerView.ViewHolder,
                ): Int {
                    return if (isEnabled && adapter?.isGlobalSearch != true) {
                        if (DataStore.groupLayoutMode == 1) {
                            ItemTouchHelper.UP or ItemTouchHelper.DOWN or ItemTouchHelper.LEFT or ItemTouchHelper.RIGHT
                        } else {
                            ItemTouchHelper.UP or ItemTouchHelper.DOWN
                        }
                    } else 0
                }

                override fun onSwiped(viewHolder: RecyclerView.ViewHolder, direction: Int) {
                }

                override fun onMove(
                    recyclerView: RecyclerView,
                    viewHolder: RecyclerView.ViewHolder, target: RecyclerView.ViewHolder,
                ): Boolean {
                    val fromPosition = viewHolder.bindingAdapterPosition
                    val toPosition = target.bindingAdapterPosition
                    
                    if (fromPosition == RecyclerView.NO_POSITION || toPosition == RecyclerView.NO_POSITION) {
                        return false
                    }
                    
                    adapter?.move(fromPosition, toPosition)
                    return true
                }

                override fun clearView(
                    recyclerView: RecyclerView,
                    viewHolder: RecyclerView.ViewHolder,
                ) {
                    super.clearView(recyclerView, viewHolder)
                    adapter?.commitMove()
                }

            })
            itemTouchHelper.attachToRecyclerView(configurationListView)
        }

        lateinit var configurationListView: RecyclerView

        val select by lazy {
            try {
                (parentFragment as ConfigurationFragment).select
            } catch (e: Exception) {
                Logs.e(e)
                false
            }
        }
        val selectedItem by lazy {
            try {
                (parentFragment as ConfigurationFragment).selectedItem
            } catch (e: Exception) {
                Logs.e(e)
                null
            }
        }

        override fun onResume() {
            super.onResume()

            if (::configurationListView.isInitialized && configurationListView.size == 0) {
                configurationListView.adapter = adapter
                runOnDefaultDispatcher {
                    adapter?.reloadProfiles()
                }
            } else if (!::configurationListView.isInitialized) {
                onViewCreated(requireView(), null)
            }
            checkOrderMenu()
            configurationListView.requestFocus()
        }

        fun checkOrderMenu() {
            if (select) return

            val pf = requireParentFragment() as? ToolbarFragment ?: return
            val menu = pf.toolbar.menu
            val origin = menu.findItem(R.id.action_order_origin)
            val byName = menu.findItem(R.id.action_order_by_name)
            val byDelay = menu.findItem(R.id.action_order_by_delay)
            when (proxyGroup.order) {
                GroupOrder.ORIGIN -> {
                    origin.isChecked = true
                }

                GroupOrder.BY_NAME -> {
                    byName.isChecked = true
                }

                GroupOrder.BY_DELAY -> {
                    byDelay.isChecked = true
                }
            }

            fun updateTo(order: Int) {
                if (proxyGroup.order == order) return
                runOnDefaultDispatcher {
                    proxyGroup.order = order
                    GroupManager.updateGroup(proxyGroup)
                }
            }

            origin.setOnMenuItemClickListener {
                it.isChecked = true
                updateTo(GroupOrder.ORIGIN)
                true
            }
            byName.setOnMenuItemClickListener {
                it.isChecked = true
                updateTo(GroupOrder.BY_NAME)
                true
            }
            byDelay.setOnMenuItemClickListener {
                it.isChecked = true
                updateTo(GroupOrder.BY_DELAY)
                true
            }
            
            val layoutSingle = menu.findItem(R.id.action_layout_single)
            val layoutDouble = menu.findItem(R.id.action_layout_double)
            when (DataStore.groupLayoutMode) {
                0 -> layoutSingle.isChecked = true
                1 -> layoutDouble.isChecked = true
            }
            layoutSingle.setOnMenuItemClickListener {
                it.isChecked = true
                if (DataStore.groupLayoutMode != 0) {
                    DataStore.groupLayoutMode = 0
                    (parentFragment as? ConfigurationFragment)?.switchAllGroupFragmentsLayout()
                }
                true
            }
            layoutDouble.setOnMenuItemClickListener {
                it.isChecked = true
                if (DataStore.groupLayoutMode != 1) {
                    DataStore.groupLayoutMode = 1
                    (parentFragment as? ConfigurationFragment)?.switchAllGroupFragmentsLayout()
                }
                true
            }

            val cardClassic = menu.findItem(R.id.action_card_style_classic)
            val cardStroke = menu.findItem(R.id.action_card_style_stroke)
            when (DataStore.profileCardStyle) {
                1 -> cardStroke.isChecked = true
                else -> cardClassic.isChecked = true
            }
            cardClassic.setOnMenuItemClickListener {
                it.isChecked = true
                if (DataStore.profileCardStyle != 0) {
                    DataStore.profileCardStyle = 0
                    (parentFragment as? ConfigurationFragment)?.refreshAllGroupFragmentsCardStyle()
                }
                true
            }
            cardStroke.setOnMenuItemClickListener {
                it.isChecked = true
                if (DataStore.profileCardStyle != 1) {
                    DataStore.profileCardStyle = 1
                    (parentFragment as? ConfigurationFragment)?.refreshAllGroupFragmentsCardStyle()
                }
                true
            }
        }

        private fun setupLayoutManager() {
            layoutManager = if (DataStore.groupLayoutMode == 1) {
                FixedGridLayoutManager(configurationListView, 2)
            } else {
                FixedLinearLayoutManager(configurationListView)
            }
        }
        
        fun switchLayoutMode() {
            setupLayoutManager()
            configurationListView.layoutManager = layoutManager
            
            setupItemTouchHelper()
            
            adapter?.notifyDataSetChanged()
        }

        override fun onViewCreated(view: View, savedInstanceState: Bundle?) {
            if (!::proxyGroup.isInitialized) return

            configurationListView = view.findViewById(R.id.configuration_list)
            configurationListView.isNestedScrollingEnabled = true
            setupLayoutManager()
            configurationListView.layoutManager = layoutManager
            adapter = ConfigurationAdapter()
            ProfileManager.addListener(adapter!!)
            GroupManager.addListener(adapter!!)
            configurationListView.adapter = adapter
            configurationListView.setItemViewCacheSize(20)

            if (!select) {
                undoManager = UndoSnackbarManager(activity as MainActivity, adapter!!)
                setupItemTouchHelper()
                setupBottomBarScrollDriver()
            }
        }

        private fun setupBottomBarScrollDriver() {
            val mainActivity = activity as? MainActivity ?: return
            configurationListView.addOnScrollListener(object : RecyclerView.OnScrollListener() {
                override fun onScrolled(recyclerView: RecyclerView, dx: Int, dy: Int) {
                    if (dy != 0) mainActivity.driveBottomBar(dy)
                    // ViewPager2's internal RecyclerView can interrupt nested pre-scroll dispatch.
                    // Drive collapse from the real virtualized list as a fallback.
                    val header = parentFragment?.view?.findViewById<com.google.android.material.appbar.AppBarLayout>(R.id.dashboard_scroll_header)
                    if (dy > 0) header?.setExpanded(false, true)
                    else if (dy < 0 && !recyclerView.canScrollVertically(-1)) header?.setExpanded(true, true)
                }
            })

            val touchSlop = ViewConfiguration.get(requireContext()).scaledTouchSlop
            var lastRawY = 0f
            configurationListView.setOnTouchListener { recyclerView, event ->
                when (event.actionMasked) {
                    MotionEvent.ACTION_DOWN -> lastRawY = event.rawY
                    MotionEvent.ACTION_MOVE -> {
                        val cannotScroll = !recyclerView.canScrollVertically(-1) &&
                                !recyclerView.canScrollVertically(1)
                        if (cannotScroll) {
                            val fingerDy = event.rawY - lastRawY
                            if (abs(fingerDy) >= touchSlop) {
                                mainActivity.driveBottomBar(-fingerDy.toInt())
                                lastRawY = event.rawY
                            }
                        }
                    }
                }
                false
            }
        }

        override fun onDestroy() {
            adapter?.let {
                ProfileManager.removeListener(it)
                GroupManager.removeListener(it)
            }

            super.onDestroy()

            if (!::undoManager.isInitialized) return
            undoManager.flush()
        }

        inner class ConfigurationAdapter : RecyclerView.Adapter<ConfigurationHolder>(),
            ProfileManager.Listener,
            GroupManager.Listener,
            UndoSnackbarManager.Interface<ProxyEntity> {

            init {
                setHasStableIds(true)
            }

            var configurationIdList: MutableList<Long> = mutableListOf()
            val configurationList = HashMap<Long, ProxyEntity>()
            private val groupTypes = HashMap<Long, Int>()
            private var searchQuery = ""
            private val searchVersion = AtomicInteger(0)
            val isGlobalSearch: Boolean
                get() = searchQuery.isNotBlank()
            private val profileStatePayload = Any()

            private fun getItem(profileId: Long): ProxyEntity {
                var profile = configurationList[profileId]
                if (profile == null) {
                    profile = ProfileManager.getProfile(profileId)
                    if (profile != null) {
                        configurationList[profileId] = profile
                    }
                }
                return profile!!
            }

            private fun getItemAt(index: Int) = getItem(configurationIdList[index])

            fun isSubscription(profile: ProxyEntity): Boolean {
                return groupTypes[profile.groupId] == GroupType.SUBSCRIPTION
            }

            private fun hasMiddleRow(p: ProxyEntity): Boolean {
                val bean = p.requireBean()
                return alwaysShowAddress && bean.name.isNotBlank() && bean.displayAddress().isNotBlank()
            }

            fun neighbourHasMiddleRow(position: Int): Boolean {
                if (position == RecyclerView.NO_POSITION) return false
                val lm = (layoutManager as? FixedGridLayoutManager) ?: return false
                val spanCount = lm.spanCount
                val rowCount = lm.rowIndexOf(position)
                var rowMax = (rowCount + 1) * spanCount - 1
                if (rowMax >= itemCount) rowMax = itemCount - 1
                var rowStart = rowCount * spanCount
                if (rowStart < 0) rowStart = 0
                for (i in rowStart..rowMax) {
                    if (i == position) continue
                    if (try {
                            hasMiddleRow(getItemAt(i))
                        } catch (e: Exception) {
                            false
                        }
                    ) return true
                }
                return false
            }

            override fun onCreateViewHolder(
                parent: ViewGroup,
                viewType: Int,
            ): ConfigurationHolder {
                return ConfigurationHolder(
                    LayoutInflater.from(parent.context)
                        .inflate(R.layout.layout_profile, parent, false)
                )
            }

            override fun getItemId(position: Int): Long {
                return configurationIdList[position]
            }

            override fun onBindViewHolder(holder: ConfigurationHolder, position: Int) {
                try {
                    holder.bind(getItemAt(position))
                } catch (ignored: NullPointerException) { // when group deleted
                }
            }

            override fun onBindViewHolder(
                holder: ConfigurationHolder,
                position: Int,
                payloads: List<Any>,
            ) {
                if (payloads.isNotEmpty() && payloads.all { it === profileStatePayload }) {
                    try {
                        holder.bindProfileState(getItemAt(position))
                    } catch (ignored: NullPointerException) { // when group deleted
                    }
                } else {
                    onBindViewHolder(holder, position)
                }
            }

            override fun onViewRecycled(holder: ConfigurationHolder) {
                holder.lastSelfHasMiddleRow = null
            }

            fun refreshSameRowNeighbours(position: Int) {
                if (position == RecyclerView.NO_POSITION) return
                val lm = (layoutManager as? FixedGridLayoutManager) ?: return
                val spanCount = lm.spanCount
                val rowCount = lm.rowIndexOf(position)
                var rowMax = (rowCount + 1) * spanCount - 1
                if (rowMax >= itemCount) rowMax = itemCount - 1
                var rowStart = rowCount * spanCount
                if (rowStart < 0) rowStart = 0
                configurationListView.post {
                    for (i in rowStart..rowMax) {
                        if (i == position) continue
                        notifyItemChanged(i)
                    }
                }
            }

            fun refreshFromPosition(startPosition: Int) {
                if (layoutManager !is FixedGridLayoutManager) return
                val start = startPosition.coerceAtLeast(0)
                if (start >= itemCount) return
                val count = itemCount - start
                configurationListView.post {
                    notifyItemRangeChanged(start, count)
                }
            }

            override fun getItemCount(): Int {
                return configurationIdList.size
            }

            fun refreshProfileState(profileIds: Set<Long>) {
                profileIds.forEach { profileId ->
                    val index = configurationIdList.indexOf(profileId)
                    if (index >= 0) notifyItemChanged(index, profileStatePayload)
                }
            }

            private val updated = HashSet<ProxyEntity>()

            fun filterGlobal(name: String) {
                searchQuery = name.trim()
                val version = searchVersion.incrementAndGet()
                runOnDefaultDispatcher {
                    val query = searchQuery
                    val profiles = selectProfilesForQuery(
                        query = query,
                        currentGroupId = proxyGroup.id,
                        allProfiles = SagerDatabase.proxyDao.getAll(),
                        groupId = ProxyEntity::groupId,
                    ) { profile, keyword ->
                        profile.displayName().contains(keyword, ignoreCase = true) ||
                            profile.displayType().contains(keyword, ignoreCase = true) ||
                            profile.displayAddress().contains(keyword, ignoreCase = true)
                    }
                    if (version != searchVersion.get()) return@runOnDefaultDispatcher
                    val orderedProfiles = if (query.isEmpty()) {
                        when (proxyGroup.order) {
                            GroupOrder.BY_NAME -> profiles.sortedBy { it.displayName() }
                            GroupOrder.BY_DELAY -> profiles.sortedBy {
                                if (it.status == 1) it.ping else 114514
                            }
                            else -> profiles.sortedBy { it.userOrder }
                        }
                    } else {
                        profiles
                    }
                    applyProfiles(orderedProfiles, scrollToSelected = query.isEmpty())
                }
            }

            fun move(from: Int, to: Int) {
                if (from == to) return

                if (layoutManager is FixedGridLayoutManager) {
                    moveDualColumn(from, to)
                } else {
                    moveLinear(from, to)
                }
            }
            
            private fun moveLinear(from: Int, to: Int) {
                val first = getItemAt(from)
                var previousOrder = first.userOrder
                val (step, range) = if (from < to) Pair(1, from until to) else Pair(
                    -1, to + 1 downTo from
                )
                for (i in range) {
                    val next = getItemAt(i + step)
                    val order = next.userOrder
                    next.userOrder = previousOrder
                    previousOrder = order
                    configurationIdList[i] = next.id
                    updated.add(next)
                }
                first.userOrder = previousOrder
                configurationIdList[to] = first.id
                updated.add(first)
                notifyItemMoved(from, to)
            }
            
            private fun moveDualColumn(from: Int, to: Int) {
                val draggedItemId = configurationIdList[from]

                configurationIdList.removeAt(from)
                configurationIdList.add(to, draggedItemId)
                
                for (i in configurationIdList.indices) {
                    val item = getItem(configurationIdList[i])
                    val newOrder = (i + 1).toLong()
                    if (item.userOrder != newOrder) {
                        item.userOrder = newOrder
                        updated.add(item)
                    }
                }
                
                notifyItemMoved(from, to)
            }

            fun commitMove() = runOnDefaultDispatcher {
                updated.forEach { SagerDatabase.proxyDao.updateProxy(it) }
                updated.clear()
                onMainDispatcher {
                    if (layoutManager is FixedGridLayoutManager) {
                        notifyDataSetChanged()
                    }
                }
            }

            fun clearTrafficStatistics() {
                for (profile in configurationList.values) {
                    if (profile.tx != 0L || profile.rx != 0L) {
                        profile.tx = 0
                        profile.rx = 0
                    }
                }
                notifyDataSetChanged()
            }

            fun clearTestResults() {
                for (profile in configurationList.values) {
                    if (profile.status != 0) {
                        profile.status = 0
                        profile.ping = 0
                        profile.error = null
                    }
                }
                notifyDataSetChanged()
            }

            fun remove(pos: Int) {
                if (pos < 0) return
                configurationIdList.removeAt(pos)
                notifyItemRemoved(pos)
                refreshFromPosition(pos - 1)
            }

            override fun undo(actions: List<Pair<Int, ProxyEntity>>) {
                for ((index, item) in actions) {
                    configurationListView.post {
                        configurationList[item.id] = item
                        configurationIdList.add(index, item.id)
                        notifyItemInserted(index)
                        refreshFromPosition(index - 1)
                    }
                }
            }

            override fun commit(actions: List<Pair<Int, ProxyEntity>>) {
                val profiles = actions.map { it.second }
                runOnDefaultDispatcher {
                    for (entity in profiles) {
                        ProfileManager.deleteProfile(entity.groupId, entity.id)
                    }
                }
            }

            override suspend fun onAdd(profile: ProxyEntity) {
                if (!isGlobalSearch && profile.groupId != proxyGroup.id) return

                if (isGlobalSearch) {
                    filterGlobal(searchQuery)
                    return
                }

                configurationListView.post {
                    if (::undoManager.isInitialized) {
                        undoManager.flush()
                    }
                    val pos = itemCount
                    configurationList[profile.id] = profile
                    configurationIdList.add(profile.id)
                    notifyItemInserted(pos)
                    refreshFromPosition(pos - 1)
                }
            }

            override suspend fun onBatchAdded(profiles: List<ProxyEntity>) {
                if (profiles.none { it.groupId == proxyGroup.id }) return
                if (isGlobalSearch) filterGlobal(searchQuery) else reloadProfiles()
            }

            override suspend fun onUpdated(profile: ProxyEntity, noTraffic: Boolean) {
                if (!isGlobalSearch && profile.groupId != proxyGroup.id) return
                if (isGlobalSearch) {
                    filterGlobal(searchQuery)
                    return
                }
                if (noTraffic) {
                    (parentFragment as? ConfigurationFragment)?.refreshProfileState()
                }
                val index = configurationIdList.indexOf(profile.id)
                if (index < 0) return
                configurationListView.post {
                    if (::undoManager.isInitialized) {
                        undoManager.flush()
                    }
                    val cachedProfile = configurationList[profile.id]
                    val updatedProfile = if (noTraffic && cachedProfile != null) {
                        profile.copy(
                            tx = cachedProfile.tx,
                            rx = cachedProfile.rx,
                        ).also { it.dirty = profile.dirty }
                    } else {
                        profile
                    }
                    val contentChanged = !noTraffic ||
                            cachedProfile == null ||
                            cachedProfile != updatedProfile ||
                            cachedProfile.dirty != updatedProfile.dirty ||
                            cachedProfile.displayName() != updatedProfile.displayName()
                    configurationList[profile.id] = updatedProfile
                    if (noTraffic && !contentChanged) return@post

                    val newHasMiddleRow = hasMiddleRow(updatedProfile)
                    val holder = layoutManager.findViewByPosition(index)
                        ?.let { configurationListView.getChildViewHolder(it) } as? ConfigurationHolder
                    val previous = holder?.lastSelfHasMiddleRow
                    notifyItemChanged(index)
                    if (previous != null && previous != newHasMiddleRow) {
                        refreshSameRowNeighbours(index)
                    }
                }
            }

            override suspend fun onUpdated(data: List<TrafficData>) {
                try {
                    onMainDispatcher {
                        for (update in data) {
                            val cached = configurationList[update.id] ?: continue
                            if (cached.tx == update.tx && cached.rx == update.rx) continue
                            cached.tx = update.tx
                            cached.rx = update.rx
                        }
                    }
                } catch (e: Exception) {
                    Logs.w(e)
                }
            }

            override suspend fun onRemoved(groupId: Long, profileId: Long) {
                if (!isGlobalSearch && groupId != proxyGroup.id) return
                val index = configurationIdList.indexOf(profileId)
                if (index < 0) return

                configurationListView.post {
                    configurationIdList.removeAt(index)
                    configurationList.remove(profileId)
                    notifyItemRemoved(index)
                    refreshFromPosition(index - 1)
                }
            }

            override suspend fun groupAdd(group: ProxyGroup) {
                if (isGlobalSearch) filterGlobal(searchQuery)
            }

            override suspend fun groupRemoved(groupId: Long) {
                if (isGlobalSearch) filterGlobal(searchQuery)
            }

            override suspend fun groupUpdated(group: ProxyGroup) {
                if (group.id == proxyGroup.id) {
                    proxyGroup = group
                } else if (!isGlobalSearch) {
                    return
                }
                if (isGlobalSearch) filterGlobal(searchQuery) else reloadProfiles()
            }

            override suspend fun groupUpdated(groupId: Long) {
                if (groupId == proxyGroup.id) {
                    proxyGroup = SagerDatabase.groupDao.getById(groupId)!!
                }
                if (isGlobalSearch) filterGlobal(searchQuery)
                else if (groupId == proxyGroup.id) reloadProfiles()
            }

            fun reloadProfiles() {
                if (isGlobalSearch) {
                    filterGlobal(searchQuery)
                    return
                }
                var newProfiles = SagerDatabase.proxyDao.getByGroup(proxyGroup.id)
                when (proxyGroup.order) {
                    GroupOrder.BY_NAME -> {
                        newProfiles = newProfiles.sortedBy { it.displayName() }

                    }

                    GroupOrder.BY_DELAY -> {
                        newProfiles =
                            newProfiles.sortedBy { if (it.status == 1) it.ping else 114514 }
                    }
                }

                applyProfiles(newProfiles, scrollToSelected = true)
            }

            private fun applyProfiles(
                newProfiles: List<ProxyEntity>,
                scrollToSelected: Boolean,
            ) {
                groupTypes.clear()
                groupTypes.putAll(
                    SagerDatabase.groupDao.allGroups().associate { it.id to it.type }
                )
                val newProfileMap = newProfiles.associateBy { it.id }
                val newProfileIds = newProfiles.map { it.id }

                var selectedProfileIndex = -1

                if (selected && scrollToSelected) {
                    val selectedProxy = selectedItem?.id ?: DataStore.selectedProxy
                    selectedProfileIndex = newProfileIds.indexOf(selectedProxy)
                }

                configurationListView.post {
                    configurationList.clear()
                    configurationList.putAll(newProfileMap)
                    configurationIdList.clear()
                    configurationIdList.addAll(newProfileIds)
                    notifyDataSetChanged()

                    if (scrollToSelected && selectedProfileIndex != -1) {
                        configurationListView.scrollTo(selectedProfileIndex, true)
                    } else if (scrollToSelected && newProfiles.isNotEmpty()) {
                        configurationListView.scrollTo(0, true)
                    }

                }
            }

        }

        val profileAccess = Mutex()
        val reloadAccess = Mutex()

        inner class ConfigurationHolder(val view: View) : RecyclerView.ViewHolder(view),
            PopupMenu.OnMenuItemClickListener {

            lateinit var entity: ProxyEntity

            var lastSelfHasMiddleRow: Boolean? = null
            private fun showShareMenu(anchor: View, proxyEntity: ProxyEntity) {
                val popup = PopupMenu(requireContext(), anchor)
                popup.menuInflater.inflate(R.menu.profile_share_menu, popup.menu)

                when {
                    !proxyEntity.haveStandardLink() -> {
                        popup.menu.findItem(R.id.action_group_qr).subMenu?.removeItem(R.id.action_standard_qr)
                        popup.menu.findItem(R.id.action_group_clipboard).subMenu?.removeItem(
                            R.id.action_standard_clipboard
                        )
                    }

                    !proxyEntity.haveLink() -> {
                        popup.menu.removeItem(R.id.action_group_qr)
                        popup.menu.removeItem(R.id.action_group_clipboard)
                    }
                }

                if (proxyEntity.nekoBean != null) {
                    popup.menu.removeItem(R.id.action_group_configuration)
                }

                popup.setOnMenuItemClickListener(this)
                popup.show()
            }

            val profileName: TextView = view.findViewById(R.id.profile_name)
            private val profileSourceGroup: TextView = view.findViewById(R.id.profile_source_group)
            val profileType: TextView = view.findViewById(R.id.profile_type)
            val profileAddress: TextView = view.findViewById(R.id.profile_address)
            val profileStatus: TextView = view.findViewById(R.id.profile_status)
            private val profileUploadSpeed: TextView = view.findViewById(R.id.profile_upload_speed)
            private val profileDownloadSpeed: TextView = view.findViewById(R.id.profile_download_speed)
            private val leafButton: View = view.findViewById(R.id.profile_leaf)
            private val speedButton: View = view.findViewById(R.id.profile_speedometer)
            private val lightningButton: View = view.findViewById(R.id.profile_lightning)

            private val card = view as MaterialCardView
            private val selectedIndicator: View = view.findViewById(R.id.selected_indicator)
            val editButton: ImageView = view.findViewById(R.id.edit)
            val doubleColumnMenuButton: ImageView = view.findViewById(R.id.double_column_menu)
            val shareLayout: LinearLayout = view.findViewById(R.id.share)
            val shareLayer: LinearLayout = view.findViewById(R.id.share_layer)
            val shareButton: ImageView = view.findViewById(R.id.shareIcon)
            val removeButton: ImageView = view.findViewById(R.id.remove)

            init {
                view.setOnClickListener {
                    val proxyEntity = entity
                    if (select) {
                        (requireActivity() as SelectCallback).returnProfile(proxyEntity.id)
                    } else {
                        selectProfile(proxyEntity)
                    }
                }
                profileStatus.setOnClickListener {
                    val proxyEntity = entity
                    if (proxyEntity.status == 3) {
                        alert(proxyEntity.error ?: "<?>").tryToShow()
                    }
                }
                profileStatus.isFocusable = false
                val configuration = parentFragment as? ConfigurationFragment
                leafButton.setOnClickListener { configuration?.showIPQualityForProfile(entity.id) }
                speedButton.setOnClickListener {
                    speedButton.playGreenPulse()
                    configuration?.startSpeedTestForProfile(entity.id, nodeSpeedTestStreams(false))
                }
                speedButton.setOnLongClickListener {
                    speedButton.playGreenPulse()
                    configuration?.startSpeedTestForProfile(entity.id, nodeSpeedTestStreams(true))
                    true
                }
                lightningButton.setOnClickListener {
                    val profile = entity
                    if (!nodeDiagnosticActions(
                            DataStore.serviceState,
                            profile.id,
                            DataStore.selectedProxy,
                            DataStore.currentProfile,
                        ).latencyEnabled
                    ) return@setOnClickListener
                    runOnDefaultDispatcher {
                        profile.status = 0
                        ProfileManager.postUpdate(profile)
                        try {
                            profile.ping = UrlTest().doTest(profile)
                            profile.status = 1
                            profile.error = null
                        } catch (e: Exception) {
                            profile.status = 3
                            profile.error = e.readableMessage
                        }
                        ProfileManager.updateProfile(profile)
                    }
                }
                editButton.setOnClickListener {
                    val proxyEntity = entity
                    it.context.startActivity(
                        proxyEntity.settingIntent(
                            it.context, proxyGroup.type == GroupType.SUBSCRIPTION
                        )
                    )
                }
                removeButton.setOnClickListener {
                    removeProfile(entity)
                }
                doubleColumnMenuButton.setOnClickListener {
                    showDoubleColumnMenu(it, entity)
                }
                doubleColumnMenuButton.setOnLongClickListener {
                    val canSpeedTest = nodeDiagnosticActions(
                        DataStore.serviceState,
                        entity.id,
                        DataStore.selectedProxy,
                        DataStore.currentProfile,
                    ).speedVisible
                    if (!canSpeedTest) return@setOnLongClickListener false
                    doubleColumnMenuButton.playGreenPulse()
                    (parentFragment as? ConfigurationFragment)
                        ?.startSpeedTestForProfile(entity.id, nodeSpeedTestStreams(true))
                    true
                }
                armManualDrag(view, this)
                shareLayout.setOnClickListener {
                    val proxyEntity = entity
                    if (!select && proxyEntity.type != ProxyEntity.TYPE_CHAIN) {
                        showShareMenu(it, proxyEntity)
                    }
                }
            }

            private fun selectProfile(proxyEntity: ProxyEntity) {
                val pf = parentFragment as? ConfigurationFragment ?: return
                runOnDefaultDispatcher {
                    var update: Boolean
                    var lastSelected: Long
                    profileAccess.withLock {
                        update = DataStore.selectedProxy != proxyEntity.id
                        lastSelected = DataStore.selectedProxy
                        DataStore.selectedProxy = proxyEntity.id
                        onMainDispatcher {
                            pf.updateSelectedProxySnapshot(proxyEntity.id)
                        }
                    }

                    if (update) {
                        ProfileManager.postUpdate(lastSelected, noTraffic = true)
                        if (DataStore.serviceState.canStop && reloadAccess.tryLock()) {
                            SagerNet.reloadService()
                            reloadAccess.unlock()
                        }
                    } else if (SagerNet.isTv) {
                        if (DataStore.serviceState.started) {
                            SagerNet.stopService()
                        } else {
                            SagerNet.startService()
                        }
                    }
                }
            }

            private fun removeProfile(proxyEntity: ProxyEntity) {
                if (select) return
                val currentAdapter = adapter ?: return
                val index = currentAdapter.configurationIdList.indexOf(proxyEntity.id)
                if (index < 0) return
                val removeAction = {
                    currentAdapter.remove(index)
                    undoManager.remove(index to proxyEntity)
                }
                if (DataStore.confirmProfileDelete) {
                    AlertDialog.Builder(requireContext())
                        .setTitle(R.string.delete_confirm_prompt)
                        .setPositiveButton(R.string.yes) { _, _ -> removeAction() }
                        .setNegativeButton(R.string.no, null)
                        .show()
                } else {
                    removeAction()
                }
            }

            private fun showDoubleColumnMenu(anchor: View, proxyEntity: ProxyEntity) {
                val popup = PopupMenu(requireContext(), anchor)
                popup.menuInflater.inflate(R.menu.double_column_item_menu, popup.menu)
                if (select) popup.menu.removeItem(R.id.action_delete)
                val actions = nodeDiagnosticActions(
                    DataStore.serviceState,
                    proxyEntity.id,
                    DataStore.selectedProxy,
                    DataStore.currentProfile,
                )
                popup.menu.findItem(R.id.action_ip_quality).isVisible = actions.qualityVisible
                popup.menu.findItem(R.id.action_speed_test).isVisible = actions.speedVisible
                popup.setForceShowIcon(true)
                popup.setOnMenuItemClickListener { menuItem ->
                    when (menuItem.itemId) {
                        R.id.action_ip_quality -> {
                            (parentFragment as? ConfigurationFragment)
                                ?.showIPQualityForProfile(proxyEntity.id)
                            true
                        }
                        R.id.action_speed_test -> {
                            (parentFragment as? ConfigurationFragment)
                                ?.startSpeedTestForProfile(proxyEntity.id, nodeSpeedTestStreams(false))
                            true
                        }
                        R.id.action_edit -> {
                            anchor.context.startActivity(
                                proxyEntity.settingIntent(
                                    anchor.context, proxyGroup.type == GroupType.SUBSCRIPTION
                                )
                            )
                            true
                        }
                        R.id.action_share -> {
                            showShareMenu(anchor, proxyEntity)
                            true
                        }
                        R.id.action_delete -> {
                            removeProfile(proxyEntity)
                            true
                        }
                        else -> false
                    }
                }
                popup.show()
            }

            private fun applySelected(selected: Boolean) {
                val ctx = card.context
                val background = ctx.getColorAttr(android.R.attr.colorBackground)
                val primary = ctx.getColorAttr(R.attr.colorPrimary)
                selectedIndicator.isVisible = selected
                card.cardElevation = 0f
                card.radius = 0f
                card.strokeWidth = 0
                card.setCardBackgroundColor(if (selected) ColorUtils.compositeColors(
                    ColorUtils.setAlphaComponent(primary, 24), background) else Color.TRANSPARENT)
            }
            private fun applyLayoutSpacing(doubleColumn: Boolean) {
                val density = resources.displayMetrics.density
                val params = card.layoutParams as? ViewGroup.MarginLayoutParams ?: return
                if (doubleColumn) {
                    params.marginStart = (2 * density).toInt()
                    params.marginEnd = (2 * density).toInt()
                    params.topMargin = (2 * density).toInt()
                    params.bottomMargin = (2 * density).toInt()
                } else {
                    val inset = resources.getDimensionPixelSize(R.dimen.lab_content_inset)
                    params.marginStart = inset
                    params.marginEnd = inset
                    params.topMargin = (4 * density).toInt()
                    params.bottomMargin = (4 * density).toInt()
                }
                card.layoutParams = params
            }

            fun bind(proxyEntity: ProxyEntity) {
                val pf = parentFragment as? ConfigurationFragment ?: return

                entity = proxyEntity
                val bean = proxyEntity.requireBean()

                profileName.text = bean.displayName()
                profileSourceGroup.isGone = true
                profileType.text = proxyEntity.displayType()
                profileType.setTextColor(requireContext().getProtocolColor(proxyEntity.type))
                val address = if (pf.alwaysShowAddress && bean.name.isNotBlank()) {
                    bean.displayAddress()
                } else ""

                profileAddress.text = address
                val addressRowEmpty = address.isBlank()
                (profileAddress.parent as View).visibility = when {
                    !addressRowEmpty -> View.VISIBLE
                    adapter?.neighbourHasMiddleRow(bindingAdapterPosition) == true -> View.INVISIBLE
                    else -> View.GONE
                }
                lastSelfHasMiddleRow = !addressRowEmpty

                profileStatus.text = if (proxyEntity.status == 1 && proxyEntity.ping >= 0) {
                    "${proxyEntity.ping} ms"
                } else ""
                profileStatus.setTextColor(requireContext().getColour(
                    if (proxyEntity.status == 1) R.color.material_green_500
                    else R.color.profile_card_secondary
                ))
                profileStatus.setOnClickListener { }
                profileStatus.isFocusable = false

                editButton.setOnClickListener {
                    if (proxyEntity.type == ProxyEntity.TYPE_XHTTP) {
                        android.widget.Toast.makeText(
                            it.context,
                            R.string.special_protocol_not_editable,
                            android.widget.Toast.LENGTH_SHORT
                        ).show()
                        return@setOnClickListener
                    }
                    try {
                        it.context.startActivity(
                            proxyEntity.settingIntent(
                                it.context, adapter?.isSubscription(proxyEntity) == true
                            )
                        )
                    } catch (e: Exception) {
                        Logs.w(e)
                        android.widget.Toast.makeText(
                            it.context,
                            getString(R.string.profile_edit_failed, e.readableMessage),
                            android.widget.Toast.LENGTH_LONG
                        ).show()
                    }
                }

                removeButton.setOnClickListener {
                    adapter?.let { adapter ->
                        val index = adapter.configurationIdList.indexOf(proxyEntity.id)
                        if (DataStore.confirmProfileDelete) {
                            AlertDialog.Builder(requireContext())
                                .setTitle(R.string.delete_confirm_prompt)
                                // .setMessage(getString(R.string.delete_confirm_prompt))
                                .setPositiveButton(R.string.yes) { dialog: DialogInterface, which: Int ->
                                    adapter.remove(index)
                                    undoManager.remove(index to proxyEntity)
                                }
                                .setNegativeButton(R.string.no, null)
                                .show()
                        } else {
                            adapter.remove(index)
                            undoManager.remove(index to proxyEntity)
                        }
                    }
                }
                
                val selectOrChain = select || proxyEntity.type == ProxyEntity.TYPE_CHAIN
                val isDoubleColumn = layoutManager is FixedGridLayoutManager
                applyLayoutSpacing(isDoubleColumn)
                
                if (isDoubleColumn) {
                    leafButton.isGone = true
                    speedButton.isGone = true
                    editButton.isGone = true
                    shareLayout.isGone = true
                    removeButton.isGone = true
                    doubleColumnMenuButton.isVisible = true
                } else {
                    shareLayout.isGone = selectOrChain
                    editButton.isGone = select
                    removeButton.isGone = select
                    doubleColumnMenuButton.isGone = true
                }

                proxyEntity.nekoBean?.apply {
                    if (!isDoubleColumn) {
                        shareLayout.isGone = true
                    }
                }

                val selected = pf.isSelectedProfile(proxyEntity.id)
                val actions = nodeDiagnosticActions(
                    DataStore.serviceState,
                    proxyEntity.id,
                    DataStore.selectedProxy,
                    DataStore.currentProfile,
                )
                lightningButton.isEnabled = actions.latencyEnabled
                leafButton.isVisible = !isDoubleColumn && actions.qualityVisible
                (leafButton as? android.widget.ImageButton)?.setColorFilter(
                    requireContext().getColour(when (pf.qualityTiers[proxyEntity.id]) {
                        "green" -> R.color.material_green_500
                        "yellow" -> R.color.material_amber_500
                        "red" -> R.color.material_red_500
                        else -> R.color.profile_card_icon
                    })
                )
                speedButton.isVisible = !isDoubleColumn && actions.speedVisible
                lightningButton.isVisible = true
                (lightningButton as? android.widget.ImageButton)?.setColorFilter(
                    requireContext().getColour(
                        if (actions.latencyEnabled) R.color.profile_card_icon else R.color.profile_card_secondary
                    )
                )
                val rates: Pair<Long, Long>? = pf.nodeRates[proxyEntity.id]
                val tested: SpeedTestCardState? = pf.speedTestRows[proxyEntity.id]
                if (tested != null) {
                    profileUploadSpeed.text = when {
                        tested.error.isNotEmpty() -> ""
                        tested.running && tested.phase == "upload" ->
                            "↑${"%.2f".format(tested.uploadCurrent)} MB/s"
                        tested.running -> ""
                        else -> "↑${"%.2f".format(tested.uploadAverage)} MB/s"
                    }
                    profileDownloadSpeed.text = when {
                        tested.running && tested.phase == "download" ->
                            "↓${"%.2f".format(tested.downloadCurrent)} MB/s"
                        tested.running && tested.downloadPeak > 0.0 ->
                            "↓${"%.2f".format(tested.downloadPeak)} MB/s"
                        tested.running -> ""
                        else -> "↓${"%.2f".format(tested.downloadAverage)} MB/s"
                    }
                } else rates?.let { (tx, rx) ->
                    profileUploadSpeed.text = "↑" + android.text.format.Formatter.formatFileSize(requireContext(), tx) + "/s"
                    profileDownloadSpeed.text = "↓" + android.text.format.Formatter.formatFileSize(requireContext(), rx) + "/s"
                } ?: run {
                    profileUploadSpeed.text = ""
                    profileDownloadSpeed.text = ""
                }
                profileUploadSpeed.isVisible = actions.speedVisible && profileUploadSpeed.text.isNotEmpty()
                profileDownloadSpeed.isVisible = actions.speedVisible && profileDownloadSpeed.text.isNotEmpty()
                val started =
                    selected && DataStore.serviceState.started && pf.isCurrentProfile(proxyEntity.id)
                editButton.isEnabled = !started
                removeButton.isEnabled = !started
                applySelected(selected)

                if (!(select || proxyEntity.type == ProxyEntity.TYPE_CHAIN)) {
                    shareLayer.setBackgroundColor(Color.TRANSPARENT)
                    shareButton.setImageResource(R.drawable.ic_social_share)
                    shareButton.setColorFilter(Color.GRAY)
                    shareButton.isVisible = true
                }

            }

            fun bindProfileState(proxyEntity: ProxyEntity) {
                if (!::entity.isInitialized || entity.id != proxyEntity.id) {
                    bind(proxyEntity)
                    return
                }
                bind(proxyEntity)
            }

            var currentName = ""
            fun showCode(link: String) {
                QRCodeDialog(link, currentName).showAllowingStateLoss(parentFragmentManager)
            }

            fun export(link: String) {
                val success = SagerNet.trySetPrimaryClip(link)
                (activity as MainActivity).snackbar(if (success) R.string.action_export_msg else R.string.action_export_err)
                    .show()
            }

            override fun onMenuItemClick(item: MenuItem): Boolean {
                try {
                    currentName = entity.displayName()!!
                    when (item.itemId) {
                        R.id.action_standard_qr -> showCode(entity.toStdLink())
                        R.id.action_standard_clipboard -> export(entity.toStdLink())
                        R.id.action_universal_qr -> showCode(entity.requireBean().toUniversalLink())
                        R.id.action_universal_clipboard -> export(
                            entity.requireBean().toUniversalLink()
                        )

                        R.id.action_config_export_clipboard -> export(entity.exportConfig().first)
                        R.id.action_config_export_file -> {
                            val cfg = entity.exportConfig()
                            DataStore.serverConfig = cfg.first
                            startFilesForResult(
                                (parentFragment as ConfigurationFragment).exportConfig, cfg.second
                            )
                        }
                    }
                } catch (e: Exception) {
                    Logs.w(e)
                    (activity as MainActivity).snackbar(e.readableMessage).show()
                    return true
                }
                return true
            }
        }

    }

    private val exportConfig =
        registerForActivityResult(ActivityResultContracts.CreateDocument()) { data ->
            if (data != null) {
                runOnDefaultDispatcher {
                    try {
                        (requireActivity() as MainActivity).contentResolver.openOutputStream(data)!!
                            .bufferedWriter()
                            .use {
                                it.write(DataStore.serverConfig)
                            }
                        onMainDispatcher {
                            snackbar(getString(R.string.action_export_msg)).show()
                        }
                    } catch (e: Exception) {
                        Logs.w(e)
                        onMainDispatcher {
                            snackbar(e.readableMessage).show()
                        }
                    }

                }
            }
        }

    private fun cancelSearch(searchView: SearchView) {
        searchView.onActionViewCollapsed()
        searchView.clearFocus()
    }

    // ==========================================
    // 🚀 DNS 黑科技区域：接管原生解析拦截
    // ==========================================

    private fun resolveDomainCustom(domain: String, dnsConfig: String?): String? {
        if (domain.isIpAddress()) return domain
        var dns = dnsConfig?.trim()
        if (dns.isNullOrEmpty() || dns == "local" || dns == "fakeip") {
            dns = "223.5.5.5" // Ultimate safe fallback
        }

        var resolved: String? = null
        if (dns!!.startsWith("https://")) {
            resolved = resolveDoH(domain, dns)
        } else {
            val cleanDns = dns.removePrefix("tcp://").removePrefix("udp://").removePrefix("tls://")
            val parts = cleanDns.split(":")
            val host = parts[0]
            val port = if (parts.size > 1) parts[1].toIntOrNull() ?: 53 else 53
            resolved = resolveUdpDns(domain, host, port)
        }

        // 终极兜底 1：阿里 DoH
        if (resolved == null) {
            resolved = resolveDoH(domain, "https://223.5.5.5/dns-query")
        }

        // 终极兜底 2：系统原生
        if (resolved == null) {
            try {
                SagerNet.underlyingNetwork!!.getAllByName(domain).apply {
                    if (isNotEmpty()) {
                        resolved = this[0].hostAddress
                    }
                }
            } catch (ignored: java.net.UnknownHostException) {
            }
        }
        return resolved
    }

    private fun resolveDoH(domain: String, url: String): String? {
        val apiUrl = when {
            url.contains("223.5.5.5") || url.contains("alidns") -> "https://223.5.5.5/resolve?name=$domain&type=1"
            url.contains("dns.google") -> "https://dns.google/resolve?name=$domain&type=1"
            url.contains("cloudflare") || url.contains("1.1.1.1") -> "https://cloudflare-dns.com/dns-query?name=$domain&type=1"
            else -> "https://223.5.5.5/resolve?name=$domain&type=1"
        }
        try {
            val conn = java.net.URL(apiUrl).openConnection() as java.net.HttpURLConnection
            conn.requestMethod = "GET"
            conn.connectTimeout = 3000
            conn.readTimeout = 3000
            conn.setRequestProperty("Accept", "application/dns-json")
            if (conn.responseCode == 200) {
                val json = org.json.JSONObject(conn.inputStream.bufferedReader().readText())
                val answers = json.optJSONArray("Answer")
                if (answers != null && answers.length() > 0) {
                    for (i in 0 until answers.length()) {
                        val ans = answers.getJSONObject(i)
                        if (ans.getInt("type") == 1) {
                            return ans.getString("data")
                        }
                    }
                }
            }
        } catch (e: Exception) {
            Logs.w("DoH resolve failed: ${e.message}")
        }
        return null
    }

    private fun resolveUdpDns(domain: String, serverIp: String, port: Int): String? {
        try {
            val socket = java.net.DatagramSocket()
            socket.soTimeout = 3000
            val baos = java.io.ByteArrayOutputStream()
            val dos = java.io.DataOutputStream(baos)

            dos.writeShort(0x1234)
            dos.writeShort(0x0100)
            dos.writeShort(1)
            dos.writeShort(0)
            dos.writeShort(0)
            dos.writeShort(0)

            for (part in domain.split(".")) {
                dos.writeByte(part.length)
                dos.writeBytes(part)
            }
            dos.writeByte(0)
            dos.writeShort(1)
            dos.writeShort(1)

            val reqData = baos.toByteArray()
            val serverAddr = if (serverIp.isIpAddress()) {
                java.net.InetAddress.getByName(serverIp)
            } else {
                java.net.InetAddress.getByName(serverIp)
            }
            val reqPacket = java.net.DatagramPacket(reqData, reqData.size, serverAddr, port)

            socket.send(reqPacket)

            val resData = ByteArray(512)
            val resPacket = java.net.DatagramPacket(resData, resData.size)
            socket.receive(resPacket)
            socket.closeQuietly()

            val dis = java.io.DataInputStream(java.io.ByteArrayInputStream(resData))
            dis.readShort()
            val flags = dis.readShort().toInt()
            if ((flags and 0x000F) != 0) return null

            val qdcount = dis.readShort().toInt()
            val ancount = dis.readShort().toInt()
            dis.readShort()
            dis.readShort()

            for (i in 0 until qdcount) {
                while (true) {
                    val len = dis.readByte().toInt() and 0xFF
                    if (len == 0) break
                    if ((len and 0xC0) == 0xC0) {
                        dis.readByte()
                        break
                    } else {
                        dis.skipBytes(len)
                    }
                }
                dis.readShort()
                dis.readShort()
            }

            for (i in 0 until ancount) {
                val nameByte = dis.readByte().toInt() and 0xFF
                if ((nameByte and 0xC0) == 0xC0) {
                    dis.readByte()
                } else {
                    var len = nameByte
                    while (len > 0) {
                        dis.skipBytes(len)
                        len = dis.readByte().toInt() and 0xFF
                    }
                }

                val type = dis.readShort().toInt()
                dis.readShort()
                dis.readInt()
                val rdlength = dis.readShort().toInt()

                if (type == 1 && rdlength == 4) {
                    val ipBytes = ByteArray(4)
                    dis.readFully(ipBytes)
                    return java.net.InetAddress.getByAddress(ipBytes).hostAddress
                } else {
                    dis.skipBytes(rdlength)
                }
            }
        } catch (e: Exception) {
            Logs.w("UDP DNS resolve failed: ${e.message}")
        }
        return null
    }

}
