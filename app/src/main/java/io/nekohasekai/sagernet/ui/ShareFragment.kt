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
            applying = true
            DataStore.allowAccess = checked
            refresh()
            if (DataStore.serviceState.started) {
                Toast.makeText(requireContext(), "正在应用共享设置…", Toast.LENGTH_SHORT).show()
                SagerNet.reloadService()
            } else {
                applying = false
            }
        }
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
        view?.findViewById<TextView>(R.id.wifi_address)?.text = wifiAddress ?: "未检测到"
        view?.findViewById<TextView>(R.id.hotspot_address)?.text = hotspotAddress ?: "未检测到"
    }
}
