package io.nekohasekai.sagernet.ui

import java.io.File
import org.junit.Assert.*
import org.junit.Test

class SnellOixUiContractTest {
    private fun source(path: String): String {
        val root = if (File("src/main").isDirectory) File(".") else File("app")
        return File(root, "src/main/$path").readText()
    }
    @Test fun everyOixFieldHasAnEditorBindingAndPreference() {
        val activity = source("java/io/nekohasekai/sagernet/ui/profile/SnellSettingsActivity.kt")
        val xml = source("res/xml/snell_preferences.xml")
        for (key in listOf("oixEchTls", "oixIdentityVersion", "oixAlpn", "oixLegacyFallback", "oixPreconnect", "oixSni", "oixConfig", "identity", "oixPath", "oixSkipCertVerify", "tcpFastOpen")) {
            assertTrue("Missing binding: $key", activity.contains("\"$key\"))"))
            assertTrue("Missing preference: $key", xml.contains("app:key=\"$key\""))
        }
        assertTrue(activity.contains("versionPref.value?.toIntOrNull() ?: 4"))
        assertFalse(activity.contains("updateOixFields(this, initialVersion)\n            true"))
    }
    @Test fun clipboardAndCurrentFileImporterBothReachRawParser() {
        assertTrue(source("java/io/nekohasekai/sagernet/ui/ConfigurationFragment.kt").contains("RawUpdater.parseRaw(text)"))
        assertTrue(source("java/io/nekohasekai/sagernet/ui/GroupSettingsActivity.kt").contains("RawUpdater.parseRaw(text, fileName)"))
    }
}
