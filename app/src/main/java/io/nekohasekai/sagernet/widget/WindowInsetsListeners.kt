package io.nekohasekai.sagernet.widget

import android.view.View
import androidx.core.view.OnApplyWindowInsetsListener
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.updatePadding

object ListListener : OnApplyWindowInsetsListener {
    override fun onApplyWindowInsets(view: View, insets: WindowInsetsCompat) = insets.apply {
        view.updatePadding(bottom = insets.getInsets(WindowInsetsCompat.Type.navigationBars()).bottom + if (view.context is io.nekohasekai.sagernet.ui.MainActivity && view.context.let { (it as io.nekohasekai.sagernet.ui.MainActivity).supportFragmentManager.findFragmentById(io.nekohasekai.sagernet.R.id.fragment_holder) is io.nekohasekai.sagernet.ui.ConfigurationFragment }) (112 * view.resources.displayMetrics.density).toInt() else 0)
    }
}
