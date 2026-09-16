package io.nekohasekai.sagernet.ui

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

class ClipboardImportPerformanceContractTest {
    private fun source(path: String): String =
        File("src/main/java/$path").readText()

    @Test
    fun bulkImportUsesSingleTransactionalApiAndPreservesInputOrder() {
        val manager = source("io/nekohasekai/sagernet/database/ProfileManager.kt")
        val fragment = source("io/nekohasekai/sagernet/ui/ConfigurationFragment.kt")
        val scanner = source("io/nekohasekai/sagernet/ui/ScannerActivity.kt")
        assertTrue(manager.contains("suspend fun createProfiles"))
        assertTrue(manager.contains("withTransaction"))
        assertTrue(manager.contains("profiles.forEachIndexed"))
        assertTrue(fragment.contains("ProfileManager.createProfiles(targetId, proxies)"))
        assertTrue(scanner.contains("ProfileManager.createProfiles(currentGroupId, results)"))
        assertFalse(fragment.contains("for (proxy in proxies) {\n            ProfileManager.createProfile"))
        assertFalse(scanner.contains("for (profile in results) {\n                        ProfileManager.createProfile"))
    }

    @Test
    fun subscriptionNavigationDoesNotWaitForRemoteAirportName() {
        val fragment = source("io/nekohasekai/sagernet/ui/ConfigurationFragment.kt")
        val clipboardBranch = fragment.substringAfter("R.id.action_import_clipboard")
            .substringBefore("R.id.action_import_file")
        assertTrue(clipboardBranch.contains("GroupSettingsActivity"))
        assertFalse(clipboardBranch.contains("withTimeoutOrNull(5_000L)"))
        assertFalse(clipboardBranch.contains("fetchAirportName(subscriptionLink)"))
    }
}
