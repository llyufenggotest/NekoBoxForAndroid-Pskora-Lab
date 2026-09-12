package io.nekohasekai.sagernet.ui

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.os.Bundle
import androidx.lifecycle.lifecycleScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import io.nekohasekai.sagernet.ktx.getColorAttr
import java.net.HttpURLConnection
import java.net.URL
import android.view.View
import android.widget.Button
import android.widget.TextView
import android.widget.Toast
import androidx.appcompat.widget.SwitchCompat
import com.google.android.material.dialog.MaterialAlertDialogBuilder
import com.google.android.material.textfield.TextInputEditText
import io.nekohasekai.sagernet.Key
import io.nekohasekai.sagernet.R
import io.nekohasekai.sagernet.SagerNet
import io.nekohasekai.sagernet.database.DataStore
import io.nekohasekai.sagernet.utils.LanAddressProvider

/** LAN sharing screen backed by the existing sing-box mixed inbound. */
class ShareFragment : ToolbarFragment(R.layout.layout_share) {
    private var wifiAddress: String? = null
    private var hotspotAddress: String? = null
    private var applying = false
    private var polling: Job? = null
    fun refreshServiceState() { applying = false; if (view != null) refresh() }

    override fun onPause() {
        polling?.cancel()
        polling = null
        super.onPause()
    }

    private suspend fun connectedClients(): String = withContext(Dispatchers.IO) {
        if (!DataStore.allowAccess || !DataStore.serviceState.connected) return@withContext "共享未运行"
        var connection: HttpURLConnection? = null
        try {
            val endpoint = io.nekohasekai.sagernet.utils.ClashConnections.endpoint(
                DataStore.configurationStore.getString("activeClashApiOptions").orEmpty()
            )
            connection = URL(endpoint.url).openConnection(java.net.Proxy.NO_PROXY) as HttpURLConnection
            connection.connectTimeout = 1500
            connection.readTimeout = 1500
            connection.instanceFollowRedirects = false
            endpoint.authorization?.let { connection.setRequestProperty("Authorization", it) }
            val code = connection.responseCode
            if (code != 200) return@withContext when (code) {
                401, 403 -> "连接监测鉴权失败（HTTP $code）；请重新连接 VPN 同步控制器 secret，不代表没有设备"
                else -> "连接监测失败（HTTP $code），不代表没有设备"
            }
            val local = java.net.NetworkInterface.getNetworkInterfaces().toList()
                .flatMap { it.inetAddresses.toList() }.mapNotNull { it.hostAddress }.toSet()
            io.nekohasekai.sagernet.utils.ClashConnections.parse(
                connection.inputStream.bufferedReader().use { it.readText() }, local
            ).describe()
        } catch (e: kotlinx.coroutines.CancellationException) {
            throw e
        } catch (e: Exception) {
            "连接监测不可用（${e.javaClass.simpleName}），不代表没有设备；请确认 Clash API 已启用并重新连接 VPN"
        } finally { connection?.disconnect() }
    }

    override fun onViewCreated(view: View, savedInstanceState: Bundle?) {
        super.onViewCreated(view, savedInstanceState)
        toolbar.title = "局域网共享"
        view.findViewById<Button>(R.id.copy_wifi).setOnClickListener { copyValue(wifiAddress) }
        view.findViewById<Button>(R.id.copy_hotspot).setOnClickListener { copyValue(hotspotAddress) }
        view.findViewById<View>(R.id.share_auth_row).setOnClickListener { showAuthenticationDialog() }
        bindSwitch()
        refresh()
    }

    override fun onResume() {
        super.onResume()
        refresh()
        polling?.cancel()
        polling = viewLifecycleOwner.lifecycleScope.launch {
            while (true) {
                refresh()
                val clients = connectedClients()
                kotlinx.coroutines.currentCoroutineContext().ensureActive()
                view?.findViewById<TextView>(R.id.share_clients)?.text = if (DataStore.allowAccess && DataStore.serviceState.connected) clients else "共享未运行"
                delay(2500)
            }
        }
    }

    private fun bindSwitch() {
        view?.findViewById<SwitchCompat>(R.id.share_switch)?.setOnCheckedChangeListener { _, checked ->
            if (applying || DataStore.allowAccess == checked) return@setOnCheckedChangeListener
            applying = DataStore.serviceState.started
            io.nekohasekai.sagernet.utils.ShareToggle.apply(
                DataStore.allowAccess, checked, DataStore.serviceState.started,
                DataStore::writeSharingPreferences, SagerNet::reloadService,
            )
            (activity as? MainActivity)?.refreshNavMenu(DataStore.enableClashAPI)
            refresh()
        }
    }

    private fun showAuthenticationDialog() {
        val dialogView = layoutInflater.inflate(R.layout.layout_share_auth_dialog, null)
        val authSwitch = dialogView.findViewById<SwitchCompat>(R.id.share_auth_switch)
        val username = dialogView.findViewById<TextInputEditText>(R.id.share_auth_username)
        val password = dialogView.findViewById<TextInputEditText>(R.id.share_auth_password)
        val enabled = DataStore.shareAuthEnabled
        authSwitch.isChecked = enabled
        username.setText(DataStore.mixedUsername)
        password.setText(if (enabled) DataStore.mixedSecret else "")
        fun updateFields() {
            username.isEnabled = authSwitch.isChecked
            password.isEnabled = authSwitch.isChecked
        }
        authSwitch.setOnCheckedChangeListener { _, _ -> updateFields() }
        updateFields()
        MaterialAlertDialogBuilder(requireContext())
            .setTitle("访问认证")
            .setView(dialogView)
            .setPositiveButton(R.string.apply) { _, _ ->
                if (authSwitch.isChecked) {
                    val user = username.text?.toString()?.trim().orEmpty()
                    val secret = password.text?.toString().orEmpty()
                    if (user.isBlank() || secret.isBlank()) {
                        Toast.makeText(requireContext(), "账号和密码不能为空", Toast.LENGTH_SHORT).show()
                        return@setPositiveButton
                    }
                    DataStore.mixedUsername = user
                    DataStore.configurationStore.putString(Key.MIXED_SECRET, secret)
                    DataStore.shareAuthEnabled = true
                } else {
                    DataStore.shareAuthEnabled = false
                }
                refresh()
                if (DataStore.serviceState.started) SagerNet.reloadService()
            }
            .setNegativeButton(android.R.string.cancel, null)
            .show()
    }

    private fun copyValue(value: String?) {
        if (value == null) return
        val clipboard = requireContext().getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
        clipboard.setPrimaryClip(ClipData.newPlainText("proxy", value))
        Toast.makeText(requireContext(), "已复制", Toast.LENGTH_SHORT).show()
    }

    private fun refresh() {
        val addresses = LanAddressProvider.current(requireContext())
        val port = DataStore.mixedPort
        wifiAddress = addresses.wifiIpv4?.let { "$it:$port" }
        hotspotAddress = addresses.hotspotRouterIpv4?.let { "$it:$port" }
        val configured = DataStore.allowAccess
        val running = configured && DataStore.serviceState.connected && !applying
        val color = if (running) android.graphics.Color.parseColor("#209C69") else android.graphics.Color.parseColor("#858B96")
        view?.findViewById<com.google.android.material.card.MaterialCardView>(R.id.share_ring)?.strokeColor = color
        view?.findViewById<android.widget.ImageView>(R.id.share_state_icon)?.apply {
            imageTintList = android.content.res.ColorStateList.valueOf(color)
            setImageResource(R.drawable.ic_share_network)
        }
        if (!DataStore.serviceState.started) applying = false

        view?.findViewById<SwitchCompat>(R.id.share_switch)?.apply {
            setOnCheckedChangeListener(null)
            isChecked = configured
            isEnabled = !applying
        }
        bindSwitch()

        view?.findViewById<TextView>(R.id.share_status)?.text = when {
            applying -> "正在重载代理服务并应用监听地址…"
            running -> "共享运行中，HTTP / SOCKS 混合代理端口 $port"
            configured -> "共享已开启；连接代理后服务将监听局域网地址"
            else -> "共享已关闭，服务不监听局域网地址"
        }
        view?.findViewById<TextView>(R.id.share_running_state)?.text = when {
            applying -> "正在应用"
            running -> "运行中"
            configured -> "等待连接"
            else -> "已停止"
        }
        view?.findViewById<TextView>(R.id.share_auth_summary)?.text = if (DataStore.shareAuthEnabled) {
            "账号 ${DataStore.mixedUsername}"
        } else {
            "无账号与密码"
        }
        view?.findViewById<TextView>(R.id.share_auth_state)?.text = if (DataStore.shareAuthEnabled) "开启" else "关闭"
        view?.findViewById<TextView>(R.id.wifi_address)?.text = wifiAddress ?: "未检测到"
        view?.findViewById<TextView>(R.id.hotspot_address)?.text = hotspotAddress ?: "未检测到"
    }
}
