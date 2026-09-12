package io.nekohasekai.sagernet.ui.profile

import android.os.Bundle
import androidx.preference.EditTextPreference
import androidx.preference.PreferenceFragmentCompat
import io.nekohasekai.sagernet.Key
import io.nekohasekai.sagernet.R
import io.nekohasekai.sagernet.database.preference.EditTextPreferenceModifiers
import io.nekohasekai.sagernet.fmt.oppa.OppaBean
import io.nekohasekai.sagernet.ktx.applyDefaultValues
import moe.matsuri.nb4a.proxy.PreferenceBinding
import moe.matsuri.nb4a.proxy.PreferenceBindingManager
import moe.matsuri.nb4a.proxy.Type

class OppaSettingsActivity : ProfileSettingsActivity<OppaBean>() {
    override fun createEntity() = OppaBean().applyDefaultValues()

    private val bindings = PreferenceBindingManager().apply {
        add(PreferenceBinding(Type.Text, "name"))
        add(PreferenceBinding(Type.Text, "serverAddress"))
        add(PreferenceBinding(Type.TextToInt, "serverPort"))
        add(PreferenceBinding(Type.Text, "password"))
        add(PreferenceBinding(Type.TextToInt, "preConnect"))
        add(PreferenceBinding(Type.Text, "sni"))
        add(PreferenceBinding(Type.Bool, "allowInsecure"))
        add(PreferenceBinding(Type.Text, "certificatePinSHA256"))
    }

    override fun OppaBean.init() = bindings.writeToCacheAll(this)

    override fun OppaBean.serialize() {
        bindings.fromCacheAll(this)
        require(password.isNotEmpty() && password.toByteArray(Charsets.UTF_8).size <= 4096) {
            getString(R.string.oppa_password_length_error)
        }
        preConnect = preConnect.coerceIn(0, 64)
    }

    override fun PreferenceFragmentCompat.createPreferences(savedInstanceState: Bundle?, rootKey: String?) {
        addPreferencesFromResource(R.xml.oppa_preferences)
        findPreference<EditTextPreference>(Key.SERVER_PORT)!!.setOnBindEditTextListener(EditTextPreferenceModifiers.Port)
        findPreference<EditTextPreference>("password")!!.summaryProvider = PasswordSummaryProvider
    }
}
