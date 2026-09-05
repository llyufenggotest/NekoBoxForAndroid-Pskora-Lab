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
import io.nekohasekai.sagernet.database.DataStore
import io.nekohasekai.sagernet.ktx.needReload
import io.nekohasekai.sagernet.utils.LanAddressProvider

/** LAN sharing screen, implemented against the existing mixed inbound. */
class ShareFragment : ToolbarFragment(R.layout.layout_share) {
    private var wifiAddress: String? = null
    private var hotspotAddress: String? = null

    override fun onViewCreated(view: View, savedInstanceState: Bundle?) {
        super.onViewCreated(view, savedInstanceState)
        toolbar.title = "共享"
        val toggle = view.findViewById<SwitchCompat>(R.id.share_switch)
        toggle.setOnCheckedChangeListener { _, checked ->
            if (DataStore.allowAccess != checked) {
                DataStore.allowAccess = checked
                refresh()
                needReload()
            }
        }
        view.findViewById<Button>(R.id.copy_wifi).setOnClickListener { copyValue(wifiAddress) }
        view.findViewById<Button>(R.id.copy_hotspot).setOnClickListener { copyValue(hotspotAddress) }
        refresh()
    }

    override fun onResume() {
        super.onResume()
        refresh()
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
        val active = DataStore.allowAccess
        view?.findViewById<SwitchCompat>(R.id.share_switch)?.apply {
            setOnCheckedChangeListener(null)
            isChecked = active
            setOnCheckedChangeListener { _, checked ->
                if (DataStore.allowAccess != checked) {
                    DataStore.allowAccess = checked
                    refresh()
                    needReload()
                }
            }
        }
        view?.findViewById<TextView>(R.id.share_status)?.text = if (active) {
            "已开启，局域网设备可使用当前代理端口 $port"
        } else {
            "已关闭，开启后允许局域网设备访问代理端口"
        }
        view?.findViewById<TextView>(R.id.wifi_address)?.text = wifiAddress ?: "未检测到"
        view?.findViewById<TextView>(R.id.hotspot_address)?.text = hotspotAddress ?: "未检测到"
    }
}
