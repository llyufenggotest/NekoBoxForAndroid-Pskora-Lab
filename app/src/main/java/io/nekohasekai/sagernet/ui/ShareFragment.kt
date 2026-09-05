package io.nekohasekai.sagernet.ui

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.os.Bundle
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

    override fun onViewCreated(view: View, savedInstanceState: Bundle?) {
        super.onViewCreated(view, savedInstanceState)
        toolbar.title = "共享"
        view.findViewById<Button>(R.id.copy_wifi).setOnClickListener { copyValue(wifiAddress) }
        view.findViewById<Button>(R.id.copy_hotspot).setOnClickListener { copyValue(hotspotAddress) }
        view.findViewById<View>(R.id.share_auth_row).setOnClickListener { showAuthenticationDialog() }
        bindSwitch()
        refresh()
    }

    override fun onResume() {
        super.onResume()
        refresh()
    }

    private fun bindSwitch() {
        view?.findViewById<SwitchCompat>(R.id.share_switch)?.setOnCheckedChangeListener { _, checked ->
            if (applying || DataStore.allowAccess == checked) return@setOnCheckedChangeListener
            applying = false
            DataStore.allowAccess = checked
            refresh()
            if (DataStore.serviceState.started) {
                SagerNet.reloadService()
                Toast.makeText(requireContext(), "共享设置已提交", Toast.LENGTH_SHORT).show()
            }
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
        val running = configured && DataStore.serviceState.connected
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
