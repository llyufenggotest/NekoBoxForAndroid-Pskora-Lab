package io.nekohasekai.sagernet.ui

import android.app.Application
import android.os.Bundle
import androidx.fragment.app.FragmentActivity
import androidx.preference.EditTextPreference
import androidx.preference.PreferenceFragmentCompat
import io.nekohasekai.sagernet.R
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [28], application = Application::class)
class GroupSettingsDnsPreferenceTest {

    @Test
    fun dnsPreferenceSetupKeepsCurrentValueVisibleWithoutSummaryProviderCrash() {
        val controller = Robolectric.buildActivity(FragmentActivity::class.java)
        val activity = controller.get().apply { setTheme(R.style.Theme_SagerNet) }
        controller.setup()
        val fragment = PreferenceHarness()
        activity.supportFragmentManager.beginTransaction()
            .add(fragment, null)
            .commitNow()

        val groupDns = fragment.findPreference<EditTextPreference>("groupCustomDns")!!
        val subscriptionDns = fragment.findPreference<EditTextPreference>("subscriptionServerDns")!!
        groupDns.text = "tls://dns.example:853"

        configureGroupDnsPreferencePresentation(groupDns, subscriptionDns)

        assertNotNull(groupDns.summaryProvider)
        assertEquals("tls://dns.example:853", groupDns.summary.toString())
        assertEquals(
            activity.getString(R.string.server_dns_subscription_sum),
            groupDns.dialogMessage,
        )
        assertFalse(subscriptionDns.isVisible)
    }

    class PreferenceHarness : PreferenceFragmentCompat() {
        override fun onCreatePreferences(savedInstanceState: Bundle?, rootKey: String?) {
            addPreferencesFromResource(R.xml.group_preferences)
        }
    }
}
