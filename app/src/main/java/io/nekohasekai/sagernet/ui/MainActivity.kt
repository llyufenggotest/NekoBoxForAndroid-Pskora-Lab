package io.nekohasekai.sagernet.ui

import androidx.lifecycle.lifecycleScope
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.coroutines.Dispatchers
import android.Manifest.permission.POST_NOTIFICATIONS
import android.annotation.SuppressLint
import android.app.ActivityManager
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.os.RemoteException
import android.view.KeyEvent
import android.view.MenuItem
import android.view.View
import androidx.activity.addCallback
import androidx.annotation.IdRes
import androidx.core.app.ActivityCompat
import androidx.core.content.ContextCompat
import androidx.preference.PreferenceDataStore
import com.google.android.material.dialog.MaterialAlertDialogBuilder
import com.google.android.material.navigation.NavigationView
import com.google.android.material.snackbar.Snackbar
import io.nekohasekai.sagernet.BuildConfig
import io.nekohasekai.sagernet.GroupType
import io.nekohasekai.sagernet.Key
import io.nekohasekai.sagernet.R
import io.nekohasekai.sagernet.SagerNet
import io.nekohasekai.sagernet.aidl.ISagerNetService
import io.nekohasekai.sagernet.aidl.SpeedDisplayData
import io.nekohasekai.sagernet.aidl.TrafficDataBatch
import io.nekohasekai.sagernet.bg.BaseService
import io.nekohasekai.sagernet.bg.SagerConnection
import io.nekohasekai.sagernet.database.DataStore
import io.nekohasekai.sagernet.database.GroupManager
import io.nekohasekai.sagernet.database.ProfileManager
import io.nekohasekai.sagernet.database.ProxyGroup
import io.nekohasekai.sagernet.database.SubscriptionBean
import io.nekohasekai.sagernet.database.preference.OnPreferenceDataStoreChangeListener
import io.nekohasekai.sagernet.databinding.LayoutMainBinding
import io.nekohasekai.sagernet.fmt.AbstractBean
import io.nekohasekai.sagernet.fmt.KryoConverters
import io.nekohasekai.sagernet.fmt.PluginEntry
import io.nekohasekai.sagernet.group.GroupInterfaceAdapter
import io.nekohasekai.sagernet.group.GroupUpdater
import io.nekohasekai.sagernet.ktx.alert
import io.nekohasekai.sagernet.ktx.isPlay
import io.nekohasekai.sagernet.ktx.isPreview
import io.nekohasekai.sagernet.ktx.launchCustomTab
import io.nekohasekai.sagernet.ktx.onMainDispatcher
import io.nekohasekai.sagernet.ktx.parseProxies
import io.nekohasekai.sagernet.ktx.readableMessage
import io.nekohasekai.sagernet.ktx.runOnDefaultDispatcher
import io.nekohasekai.sagernet.ui.MessageStore
import io.nekohasekai.sagernet.ktx.Logs
import moe.matsuri.nb4a.utils.Util

class MainActivity : ThemedActivity(),
    SagerConnection.Callback,
    OnPreferenceDataStoreChangeListener,
    NavigationView.OnNavigationItemSelectedListener {

    lateinit var binding: LayoutMainBinding
    lateinit var navigation: NavigationView
    private var currentMainFragment: ToolbarFragment? = null
    private var lastUiState = BaseService.State.Idle
    private var metricsTarget = 0L
    private var metricsGeneration = 0L
    private var latency: Int? = null
    internal val dashboardHealth = DashboardHealth()
    private val connectionTestResult get() = dashboardHealth.result
    private var upload: Long? = null
    private var download: Long? = null
    private fun targetProfile(): Long = if (DataStore.serviceState.started && DataStore.currentProfile > 0)
        DataStore.currentProfile else DataStore.selectedProxy

    private fun refreshVpnStrip() {
        val target = targetProfile()
        if (metricsTarget != target) {
            metricsTarget = target
            metricsGeneration++
            latency = null; upload = null; download = null
        }
        val generation = metricsGeneration
        dashboardHealth.serviceChanged(target, DataStore.serviceState.connected)
        binding.vpnNodeName.text = "加载节点…"
        lifecycleScope.launch {
            val name = withContext(Dispatchers.IO) {
                ProfileManager.getProfile(target)?.displayName() ?: "未选择节点"
            }
            if (generation == metricsGeneration && target == targetProfile()) binding.vpnNodeName.text = name
        }
        renderVpnMetrics()
    }

    private fun renderVpnMetrics() {
        val tone = dashboardHealth.tone
        binding.fab.changeState(
            if (tone == DashboardHealth.Tone.GREEN) BaseService.State.Connected else BaseService.State.Stopped,
            BaseService.State.Stopped, false
        )
        binding.fab.backgroundTintList = android.content.res.ColorStateList.valueOf(ContextCompat.getColor(this, tone.background))
        binding.fab.imageTintList = android.content.res.ColorStateList.valueOf(ContextCompat.getColor(this, tone.foreground))
        binding.fab.isEnabled = DataStore.serviceState != BaseService.State.Stopping
        binding.fab.contentDescription = if (DataStore.serviceState.canStop) "断开 VPN" else "连接 VPN"
        val state = when (DataStore.serviceState) {
            BaseService.State.Connected -> when (dashboardHealth.tone) {
                DashboardHealth.Tone.GREEN -> "已连接"
                DashboardHealth.Tone.RED -> "连接失败"
                DashboardHealth.Tone.GRAY -> "正在测试"
            }
            BaseService.State.Connecting -> "连接中"
            BaseService.State.Stopping -> "断开中"
            else -> "未连接"
        }
        fun rate(value: Long?) = value?.let { android.text.format.Formatter.formatFileSize(this, it) + "/s" } ?: "—"
        val testStatus = when (val result = connectionTestResult) {
            DashboardConnectionTestResult.Testing -> "测试中…"
            is DashboardConnectionTestResult.Success -> "${result.elapsedMs}ms"
            DashboardConnectionTestResult.Timeout -> "连接超时 · 点击重试"
            is DashboardConnectionTestResult.Failure -> "${result.reason} · 点击重试"
            null -> latency?.let { "${it}ms" } ?: "待测"
        }
        binding.vpnMetrics.text = "$state · $testStatus ↑ ${rate(upload)} ↓ ${rate(download)}"
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        MessageStore.setCurrentActivity(this)
        val animateInitialControls = savedInstanceState == null

        binding = LayoutMainBinding.inflate(layoutInflater)
        binding.fab.initProgress(binding.fabProgress)
        lifecycleScope.launch {
            io.nekohasekai.sagernet.utils.DefaultNetworkListener.start(this@MainActivity) { network ->
                lifecycleScope.launch {
                    val caps = network?.let { io.nekohasekai.sagernet.SagerNet.connectivity.getNetworkCapabilities(it) }
                    if (caps?.hasTransport(android.net.NetworkCapabilities.TRANSPORT_VPN) == true) return@launch
                    val lp = network?.let { io.nekohasekai.sagernet.SagerNet.connectivity.getLinkProperties(it) }
                    // Include every native reset-relevant property, with stable
                    // collection ordering; DNS/IP/gateway/capability changes need
                    // a fresh real probe even when the interface name is unchanged.
                    val signature = listOf(network, lp?.interfaceName, lp?.mtu,
                        lp?.linkAddresses?.map { it.toString() }?.sorted(),
                        lp?.dnsServers?.map { it.hostAddress ?: "" }?.sorted(),
                        lp?.routes?.map { it.toString() }?.sorted(),
                        caps?.hasTransport(android.net.NetworkCapabilities.TRANSPORT_WIFI),
                        caps?.hasTransport(android.net.NetworkCapabilities.TRANSPORT_CELLULAR),
                        caps?.hasTransport(android.net.NetworkCapabilities.TRANSPORT_ETHERNET),
                        caps?.hasCapability(android.net.NetworkCapabilities.NET_CAPABILITY_NOT_METERED),
                        if (android.os.Build.VERSION.SDK_INT >= 28) caps?.hasCapability(android.net.NetworkCapabilities.NET_CAPABILITY_NOT_CONGESTED) else null
                    ).joinToString(":")
                    if (dashboardHealth.networkChanged(signature)) {
                        renderVpnMetrics()
                        if (!dashboardProbeInFlight && DataStore.serviceState.connected) runDashboardConnectionTest()
                    }
                }
            }
        }
        // The Activity owns system insets; the navigation widget must not add an opaque inset scrim.
        androidx.core.view.ViewCompat.setOnApplyWindowInsetsListener(binding.bottomNavigation) { v, _ ->
            v.setPadding(0, 0, 0, 0)
            androidx.core.view.WindowInsetsCompat.CONSUMED
        }
        binding.bottomNavigation.backgroundTintList = android.content.res.ColorStateList.valueOf(android.graphics.Color.TRANSPARENT)
        binding.bottomNavigation.setBackgroundColor(android.graphics.Color.TRANSPARENT)
        binding.bottomNavigation.isItemActiveIndicatorEnabled = false
        (binding.bottomNavigation.getChildAt(0) as? android.view.ViewGroup)?.setBackgroundColor(android.graphics.Color.TRANSPARENT)
        // Keep the capsule and VPN strip above both gesture and three-button navigation.
        androidx.core.view.ViewCompat.setOnApplyWindowInsetsListener(binding.coordinator) { _, insets ->
            val bottom = insets.getInsets(androidx.core.view.WindowInsetsCompat.Type.navigationBars()).bottom
            fun bottomMargin(view: View, dp: Int) {
                val params = view.layoutParams as androidx.coordinatorlayout.widget.CoordinatorLayout.LayoutParams
                params.bottomMargin = bottom + (dp * resources.displayMetrics.density).toInt()
                view.layoutParams = params
            }
            bottomMargin(binding.navigationCapsule, 12)
            bottomMargin(binding.vpnControlStrip, 84)
            bottomMargin(binding.fab, 88)
            val fragment = currentMainFragment ?: supportFragmentManager.findFragmentById(R.id.fragment_holder)
            if (fragment !is ConfigurationFragment) bottomMargin(binding.fragmentHolder, 144)
            insets
        }
        if (themeResId !in intArrayOf(
                R.style.Theme_SagerNet_Black
            )
        ) {
            navigation = binding.navView
            binding.drawerLayout.removeView(binding.navViewBlack)
        } else {
            navigation = binding.navViewBlack
            binding.drawerLayout.removeView(binding.navView)
        }
        navigation.setNavigationItemSelectedListener(this)
        binding.bottomNavigation.setOnItemSelectedListener { item ->
            when (item.itemId) {
                R.id.bottom_home -> displayFragmentWithId(R.id.nav_configuration)
                R.id.bottom_route -> displayFragmentWithId(R.id.nav_route)
                R.id.bottom_share -> displayFragmentWithId(R.id.nav_share)
                R.id.bottom_settings -> displayFragmentWithId(R.id.nav_settings)
                else -> false
            }
        }

        if (savedInstanceState == null) {
            displayFragmentWithId(R.id.nav_configuration)
        } else {
            currentMainFragment =
                supportFragmentManager.findFragmentById(R.id.fragment_holder) as? ToolbarFragment
        }
        onBackPressedDispatcher.addCallback {
            if (supportFragmentManager.findFragmentById(R.id.fragment_holder) is ConfigurationFragment) {
                moveTaskToBack(true)
            } else {
                displayFragmentWithId(R.id.nav_configuration)
            }
        }

        binding.fab.setOnClickListener {
            if (DataStore.serviceState.canStop) stopDashboardConnection() else requestDashboardConnection()
        }
        binding.stats.setOnClickListener { if (DataStore.serviceState.connected) binding.stats.testConnection() }
        binding.vpnControlStrip.setOnClickListener { runDashboardConnectionTest() }

        setContentView(binding.root)
        currentMainFragment =
            supportFragmentManager.findFragmentById(R.id.fragment_holder) as? ToolbarFragment
                ?: currentMainFragment
        if (!animateInitialControls) {
            syncMainControls(showWhenConnected = false, animate = false)
        }
        changeState(
            BaseService.State.Idle,
            animate = false,
            animateControls = animateInitialControls,
        )
        connection.connect(this, this)
        DataStore.configurationStore.registerChangeListener(this)
        GroupManager.userInterface = GroupInterfaceAdapter(this)

        if (intent?.action == Intent.ACTION_VIEW) {
            onNewIntent(intent)
        }

        refreshNavMenu(DataStore.enableClashAPI)

        // sdk 33 notification
        if (Build.VERSION.SDK_INT >= 33) {
            val checkPermission =
                ContextCompat.checkSelfPermission(this@MainActivity, POST_NOTIFICATIONS)
            if (checkPermission != PackageManager.PERMISSION_GRANTED) {
                //动态申请
                ActivityCompat.requestPermissions(
                    this@MainActivity, arrayOf(POST_NOTIFICATIONS), 0
                )
            }
        }

        if (isPreview) {
            MaterialAlertDialogBuilder(this)
                .setTitle(BuildConfig.PRE_VERSION_NAME)
                .setMessage(R.string.preview_version_hint)
                .setPositiveButton(android.R.string.ok, null)
                .show()
        }
    }

    override fun onResume() {
        super.onResume()
        MessageStore.setCurrentActivity(this)

        if (DataStore.hideFromRecentApps) {
            applyHideFromRecentApps(DataStore.hideFromRecentApps)
        }
    }

    override fun onPostResume() {
        super.onPostResume()
        val restoredFragment =
            supportFragmentManager.findFragmentById(R.id.fragment_holder) as? ToolbarFragment
        if (restoredFragment != null && restoredFragment !== currentMainFragment) {
            currentMainFragment = restoredFragment
            syncMainControls(
                fragment = restoredFragment,
                showWhenConnected = DataStore.serviceState == BaseService.State.Connected,
                animate = false,
            )
        }
    }

    fun applyHideFromRecentApps(hide: Boolean) {
        try {
            val activityManager = getSystemService(Context.ACTIVITY_SERVICE) as ActivityManager
            val tasks = activityManager.appTasks
            if (tasks.isNotEmpty()) {
                val task = tasks[0]
                task.setExcludeFromRecents(hide)
            }
        } catch (e: Exception) {
            Logs.w("Failed to set excludeFromRecents: ${e.message}")
        }
    }

    fun refreshNavMenu(clashApi: Boolean) {
        if (::navigation.isInitialized) {
            navigation.menu.findItem(R.id.nav_traffic)?.isVisible = clashApi
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)

        val uri = intent.data ?: return

        runOnDefaultDispatcher {
            if (uri.scheme == "sn" && uri.host == "subscription" || uri.scheme == "clash") {
                importSubscription(uri)
            } else {
                importProfile(uri)
            }
        }
    }

    fun urlTest(): Int {
        if (!DataStore.serviceState.connected || connection.service == null) {
            error("not started")
        }
        return connection.service!!.urlTest()
    }

    suspend fun importSubscription(uri: Uri) {
        val group: ProxyGroup

        val url = uri.getQueryParameter("url")
        if (!url.isNullOrBlank()) {
            group = ProxyGroup(type = GroupType.SUBSCRIPTION)
            val subscription = SubscriptionBean()
            group.subscription = subscription

            // cleartext format
            subscription.link = url
            group.name = uri.getQueryParameter("name")
        } else {
            val data = uri.encodedQuery.takeIf { !it.isNullOrBlank() } ?: return
            try {
                group = KryoConverters.deserialize(
                    ProxyGroup().apply { export = true }, Util.zlibDecompress(Util.b64Decode(data))
                ).apply {
                    export = false
                }
            } catch (e: Exception) {
                onMainDispatcher {
                    alert(e.readableMessage).show()
                }
                return
            }
        }

        val name = group.name.takeIf { !it.isNullOrBlank() } ?: group.subscription?.link
        ?: group.subscription?.token
        if (name.isNullOrBlank()) return

        group.name = group.name.takeIf { !it.isNullOrBlank() }
            ?: ("Subscription #" + System.currentTimeMillis())

        onMainDispatcher {

            displayFragmentWithId(R.id.nav_group)

            MaterialAlertDialogBuilder(this@MainActivity).setTitle(R.string.subscription_import)
                .setMessage(getString(R.string.subscription_import_message, name))
                .setPositiveButton(R.string.yes) { _, _ ->
                    runOnDefaultDispatcher {
                        finishImportSubscription(group)
                    }
                }
                .setNegativeButton(android.R.string.cancel, null)
                .show()

        }

    }

    private suspend fun finishImportSubscription(subscription: ProxyGroup) {
        GroupManager.createGroup(subscription)
        GroupUpdater.startUpdate(subscription, true)
    }

    suspend fun importProfile(uri: Uri) {
        val profile = try {
            parseProxies(uri.toString()).getOrNull(0) ?: error(getString(R.string.no_proxies_found))
        } catch (e: Exception) {
            onMainDispatcher {
                alert(e.readableMessage).show()
            }
            return
        }

        onMainDispatcher {
            MaterialAlertDialogBuilder(this@MainActivity).setTitle(R.string.profile_import)
                .setMessage(getString(R.string.profile_import_message, profile.displayName()))
                .setPositiveButton(R.string.yes) { _, _ ->
                    runOnDefaultDispatcher {
                        finishImportProfile(profile)
                    }
                }
                .setNegativeButton(android.R.string.cancel, null)
                .show()
        }

    }

    private suspend fun finishImportProfile(profile: AbstractBean) {
        val targetId = DataStore.selectedGroupForImport()

        ProfileManager.createProfile(targetId, profile)

        onMainDispatcher {
            displayFragmentWithId(R.id.nav_configuration)

            snackbar(resources.getQuantityString(R.plurals.added, 1, 1)).show()
        }
    }

    override fun missingPlugin(profileName: String, pluginName: String) {
        val pluginEntity = PluginEntry.find(pluginName)

        // unknown exe or neko plugin
        if (pluginEntity == null) {
            snackbar(getString(R.string.plugin_unknown, pluginName)).show()
            return
        }

        // official exe

        MaterialAlertDialogBuilder(this).setTitle(R.string.missing_plugin)
            .setMessage(
                getString(
                    R.string.profile_requiring_plugin, profileName, pluginEntity.displayName
                )
            )
            .setPositiveButton(R.string.action_download) { _, _ ->
                showDownloadDialog(pluginEntity)
            }
            .setNeutralButton(android.R.string.cancel, null)
            .setNeutralButton(R.string.action_learn_more) { _, _ ->
                launchCustomTab("https://matsuridayo.github.io/nb4a-plugin/")
            }
            .show()
    }

    private fun showDownloadDialog(pluginEntry: PluginEntry) {
        var index = 0
        var playIndex = -1
        var fdroidIndex = -1

        val items = mutableListOf<String>()
        if (pluginEntry.downloadSource.playStore) {
            items.add(getString(R.string.install_from_play_store))
            playIndex = index++
        }
        if (pluginEntry.downloadSource.fdroid) {
            items.add(getString(R.string.install_from_fdroid))
            fdroidIndex = index++
        }

        items.add(getString(R.string.download))
        val downloadIndex = index

        MaterialAlertDialogBuilder(this).setTitle(pluginEntry.name)
            .setItems(items.toTypedArray()) { _, which ->
                when (which) {
                    playIndex -> launchCustomTab("https://play.google.com/store/apps/details?id=${pluginEntry.packageName}")
                    fdroidIndex -> launchCustomTab("https://f-droid.org/packages/${pluginEntry.packageName}/")
                    downloadIndex -> launchCustomTab(pluginEntry.downloadSource.downloadLink)
                }
            }
            .show()
    }

    override fun onNavigationItemSelected(item: MenuItem): Boolean {
        if (item.isChecked) binding.drawerLayout.closeDrawers() else {
            return displayFragmentWithId(item.itemId)
        }
        return true
    }


    @SuppressLint("CommitTransaction")
    fun displayFragment(fragment: ToolbarFragment) {
        currentMainFragment = fragment
        supportFragmentManager.beginTransaction()
            .replace(R.id.fragment_holder, fragment)
            .commitAllowingStateLoss()
        binding.drawerLayout.closeDrawers()
        syncMainControls(fragment, showWhenConnected = false, animate = true)
    }

    private var connectionWatchdog: kotlinx.coroutines.Job? = null
    // Binder calls are blocking: keep one in flight even after the UI timeout.
    private var dashboardProbeInFlight = false

    private fun armConnectionWatchdog() {
        connectionWatchdog?.cancel()
        connectionWatchdog = lifecycleScope.launch {
            kotlinx.coroutines.delay(30_000)
            if (DataStore.serviceState == BaseService.State.Connecting) {
                dashboardHealth.serviceChanged(targetProfile(), false, DashboardConnectionTestResult.Timeout)
                renderVpnMetrics()
                refreshConfigurationProfileState()
                SagerNet.stopService()
            }
        }
    }

    fun requestDashboardConnection() {
        dashboardHealth.reset()
        renderVpnMetrics()
        refreshConfigurationProfileState()
        connect.launch(null)
    }

    fun stopDashboardConnection() {
        connectionWatchdog?.cancel()
        dashboardHealth.reset()
        renderVpnMetrics()
        refreshConfigurationProfileState()
        SagerNet.stopService()
    }

    fun runDashboardConnectionTest() {
        if (!DataStore.serviceState.connected) {
            snackbar("请先连接 VPN").show()
            return
        }
        if (dashboardProbeInFlight) {
            snackbar("上一次检测尚未结束，请稍候重试").show()
            return
        }
        val target = targetProfile()
        dashboardHealth.serviceChanged(target, true)
        val ticket = dashboardHealth.beginTest(target) ?: return
        dashboardProbeInFlight = true
        latency = null
        renderVpnMetrics()
        (currentMainFragment as? ConfigurationFragment)?.updateDashboardConnectionTest(
            target,
            DashboardConnectionTestResult.Testing
        )
        val testTimeout = lifecycleScope.launch {
            kotlinx.coroutines.delay(15_000)
            if (DataStore.serviceState.connected && target == targetProfile() &&
                dashboardHealth.complete(ticket, DashboardConnectionTestResult.Timeout)) {
                renderVpnMetrics()
                refreshConfigurationProfileState()
            }
        }
        lifecycleScope.launch {
            val result = try {
                val elapsed = withContext(Dispatchers.IO) { urlTest() }
                if (elapsed >= 0) DashboardConnectionTestResult.Success(elapsed)
                else DashboardConnectionTestResult.Failure("测试未返回有效延迟")
            } catch (e: kotlinx.coroutines.CancellationException) {
                throw e
            } catch (e: Exception) {
                dashboardConnectionTestFailure(e)
            } finally {
                dashboardProbeInFlight = false
            }
            testTimeout.cancel()
            if (DataStore.serviceState.connected && dashboardHealth.result == null) {
                runDashboardConnectionTest()
                return@launch
            }
            if (!DataStore.serviceState.connected || target != targetProfile() || !dashboardHealth.complete(ticket, result)) return@launch
            latency = (result as? DashboardConnectionTestResult.Success)?.elapsedMs
            renderVpnMetrics()
            (currentMainFragment as? ConfigurationFragment)?.updateDashboardConnectionTest(target, result)
        }
    }

    private fun syncMainControls(
        fragment: Any? = currentMainFragment
            ?: supportFragmentManager.findFragmentById(R.id.fragment_holder),
        showWhenConnected: Boolean,
        animate: Boolean,
    ) {
        val dashboardHome = fragment is ConfigurationFragment
        val showControls = !dashboardHome
        val holder = binding.fragmentHolder
        // Bottom controls occupy their own compact strip; never push the toolbar down.
                holder.setPadding(0, 0, 0, 0)
                (holder.layoutParams as androidx.coordinatorlayout.widget.CoordinatorLayout.LayoutParams).apply {
            bottomMargin = if (dashboardHome) 0 else
                (144 * resources.displayMetrics.density).toInt() +
                    (androidx.core.view.ViewCompat.getRootWindowInsets(holder)
                        ?.getInsets(androidx.core.view.WindowInsetsCompat.Type.navigationBars())?.bottom ?: 0)
                    holder.layoutParams = this
                }
                binding.vpnControlStrip.visibility = if (showControls) View.VISIBLE else View.GONE
                refreshVpnStrip()
                // A translated BottomAppBar still draws an opaque rectangle under the capsule.
                binding.stats.visibility = View.GONE
        binding.stats.useExternalScrollDriver = false
        binding.stats.syncMainControls(
            false,
            DataStore.serviceState,
            showWhenConnected,
            animate,
        )
        binding.fab.animate().cancel()
        if (showControls) {
            binding.fab.show()
        } else {
            binding.fab.hideProgress()
            binding.fabProgress.hide()
            binding.fabProgress.visibility = View.INVISIBLE
            if (animate && binding.fab.isLaidOut) {
                binding.fab.hide()
            } else {
                binding.fab.visibility = View.INVISIBLE
            }
        }
    }

    private fun refreshConfigurationProfileState() {
        val fragment = currentMainFragment
            ?: supportFragmentManager.findFragmentById(R.id.fragment_holder)
        (fragment as? ConfigurationFragment)?.refreshProfileState()
    }

    fun driveBottomBar(scrollDy: Int) {
        binding.stats.onListScrolled(scrollDy)
    }

    fun displayFragmentWithId(@IdRes id: Int): Boolean {
        when (id) {
            R.id.nav_configuration -> {
                displayFragment(ConfigurationFragment())
            }

            R.id.nav_group -> displayFragment(GroupFragment())
            R.id.nav_route -> displayFragment(RouteFragment())
            R.id.nav_share -> displayFragment(ShareFragment())
            R.id.nav_settings -> displayFragment(SettingsFragment())
            R.id.nav_traffic -> displayFragment(WebviewFragment())
            R.id.nav_tools -> displayFragment(ToolsFragment())
            R.id.nav_logcat -> displayFragment(LogcatFragment())
            R.id.nav_faq -> {
                launchCustomTab("https://matsuridayo.github.io/")
                return false
            }

            R.id.nav_about -> displayFragment(AboutFragment())

            else -> return false
        }
        navigation.menu.findItem(id).isChecked = true
        return true
    }

    private fun changeState(
        state: BaseService.State,
        msg: String? = null,
        animate: Boolean = false,
        animateControls: Boolean = animate,
    ) {
        val previousState = lastUiState
        if (previousState != state) {
            if (state == BaseService.State.Connecting) armConnectionWatchdog()
            else connectionWatchdog?.cancel()
        }
        lastUiState = state
        DataStore.serviceState = state
        if (previousState != state) {
            metricsGeneration++
            latency = null; upload = null; download = null
        }
        dashboardHealth.serviceChanged(targetProfile(), state.connected,
            msg?.let { dashboardConnectionTestFailure(IllegalStateException(it)) })
        refreshConfigurationProfileState()
        (currentMainFragment as? ShareFragment)?.refreshServiceState()
        if (state == BaseService.State.Connected && previousState != state) {
            binding.root.post { runDashboardConnectionTest() }
        }

        // Health rendering below owns the button color and ring.
        binding.stats.changeState(state)
        syncMainControls(
            showWhenConnected = state == BaseService.State.Connected,
            animate = animateControls,
        )
        if (msg != null) snackbar(getString(R.string.vpn_error, msg)).show()
    }

    override fun snackbarInternal(text: CharSequence): Snackbar {
        return Snackbar.make(binding.coordinator, text, Snackbar.LENGTH_LONG).apply {
            if (binding.fab.isShown) {
                anchorView = binding.fab
            }
            // TODO
        }
    }

    override fun stateChanged(state: BaseService.State, profileName: String?, msg: String?) {
        if (state != BaseService.State.Connected) {
            (currentMainFragment as? ConfigurationFragment)?.clearDiagnosticSessions()
        }
        changeState(state, msg, true)
    }

    val connection = SagerConnection(SagerConnection.CONNECTION_ID_MAIN_ACTIVITY_FOREGROUND, true)
    override fun onServiceConnected(service: ISagerNetService) = changeState(
        try {
            BaseService.State.values()[service.state]
        } catch (_: RemoteException) {
            BaseService.State.Idle
        }
    )

    override fun onServiceDisconnected() = changeState(BaseService.State.Idle,
        if (DataStore.serviceState.started) "VPN 服务连接丢失" else null)
    override fun onBinderDied() {
        connection.disconnect(this)
        connection.connect(this, this)
    }

    private val connect = registerForActivityResult(VpnRequestActivity.StartService()) {
        if (it) snackbar(R.string.vpn_permission_denied).show()
    }

    // may NOT called when app is in background
    // ONLY do UI update here, write DB in bg process
    override fun cbSpeedUpdate(stats: SpeedDisplayData) {
        if (metricsTarget != targetProfile()) refreshVpnStrip()
        if (DataStore.serviceState.connected) {
            upload = stats.txRateProxy; download = stats.rxRateProxy
            renderVpnMetrics()
        }
        binding.stats.updateSpeed(stats.txRateProxy, stats.rxRateProxy)
        (currentMainFragment as? ConfigurationFragment)?.updateDashboardSpeed(
            targetProfile(),
            stats.txRateProxy,
            stats.rxRateProxy,
            stats.txTotal,
            stats.rxTotal,
        )
    }

    override suspend fun cbTrafficUpdate(data: TrafficDataBatch) {
        ProfileManager.postUpdate(data.items)
    }

    override fun cbSpeedTestProgress(
        profileId: Long, phase: String, currentMBps: Double,
        peakMBps: Double, transferredBytes: Long,
    ) {
        (currentMainFragment as? ConfigurationFragment)
            ?.updateSpeedTestProgress(profileId, phase, currentMBps, peakMBps)
    }

    override fun cbSpeedTestComplete(profileId: Long, downloadMBps: Double, uploadMBps: Double) {
        (currentMainFragment as? ConfigurationFragment)
            ?.updateSpeedTestComplete(profileId, downloadMBps, uploadMBps)
    }

    override fun cbSpeedTestError(profileId: Long, message: String) {
        (currentMainFragment as? ConfigurationFragment)?.updateSpeedTestError(profileId, message)
        snackbar(message).show()
    }

    override fun cbSelectorUpdate(id: Long) {
        val old = DataStore.selectedProxy
        DataStore.selectedProxy = id
        DataStore.currentProfile = id
        refreshVpnStrip()
        if (DataStore.serviceState.connected) runDashboardConnectionTest()
        refreshConfigurationProfileState()
        runOnDefaultDispatcher {
            ProfileManager.postUpdate(old, true)
            ProfileManager.postUpdate(id, true)
        }
    }

    override fun onPreferenceDataStoreChanged(store: PreferenceDataStore, key: String) {
        when (key) {
            Key.PROFILE_ID, Key.PROFILE_CURRENT -> lifecycleScope.launch {
                refreshVpnStrip()
                refreshConfigurationProfileState()
                if (DataStore.serviceState.connected && dashboardHealth.result == null) runDashboardConnectionTest()
            }
            Key.ENABLE_CLASH_API -> lifecycleScope.launch { refreshNavMenu(DataStore.enableClashAPI) }
            Key.SERVICE_MODE -> onBinderDied()
            Key.SHOW_BOTTOM_BAR -> syncMainControls(
                showWhenConnected = DataStore.showBottomBar,
                animate = true,
            )
            Key.PROXY_APPS, Key.BYPASS_MODE, Key.INDIVIDUAL -> {
                if (DataStore.serviceState.canStop) {
                    snackbar(getString(R.string.need_reload)).setAction(R.string.apply) {
                        SagerNet.reloadService()
                    }.show()
                }
            }
        }
    }

    override fun onStart() {
        connection.updateConnectionId(SagerConnection.CONNECTION_ID_MAIN_ACTIVITY_FOREGROUND)
        super.onStart()
    }

    override fun onStop() {
        connection.updateConnectionId(SagerConnection.CONNECTION_ID_MAIN_ACTIVITY_BACKGROUND)
        super.onStop()
    }

    override fun onDestroy() {
        kotlinx.coroutines.runBlocking { io.nekohasekai.sagernet.utils.DefaultNetworkListener.stop(this@MainActivity) }
        super.onDestroy()
        GroupManager.userInterface = null
        DataStore.configurationStore.unregisterChangeListener(this)
        connection.disconnect(this)
    }

    override fun onKeyDown(keyCode: Int, event: KeyEvent): Boolean {
        when (keyCode) {
            KeyEvent.KEYCODE_DPAD_LEFT -> {
                if (super.onKeyDown(keyCode, event)) return true
                binding.drawerLayout.open()
                navigation.requestFocus()
            }

            KeyEvent.KEYCODE_DPAD_RIGHT -> {
                if (binding.drawerLayout.isOpen) {
                    binding.drawerLayout.close()
                    return true
                }
            }
        }

        if (super.onKeyDown(keyCode, event)) return true
        if (binding.drawerLayout.isOpen) return false

        val fragment =
            supportFragmentManager.findFragmentById(R.id.fragment_holder) as? ToolbarFragment
        return fragment != null && fragment.onKeyDown(keyCode, event)
    }

}
