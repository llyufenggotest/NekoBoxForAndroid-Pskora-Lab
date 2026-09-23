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
        assertTrue(manager.contains("val ids = SagerDatabase.proxyDao.insert(profiles)"))
        assertTrue(manager.contains("profile.id = ids[index]"))
        assertTrue(fragment.contains("ProfileManager.createProfiles(targetId, proxies)"))
        assertTrue(scanner.contains("ProfileManager.createProfiles(currentGroupId, results)"))
        assertFalse(fragment.contains("for (proxy in proxies) {\n            ProfileManager.createProfile"))
        assertFalse(scanner.contains("for (profile in results) {\n                        ProfileManager.createProfile"))
    }

    @Test
    fun bulkImportRefreshesEveryProfileInTheVisibleGroup() {
        val fragment = source("io/nekohasekai/sagernet/ui/ConfigurationFragment.kt")
        assertTrue(fragment.contains("override suspend fun onBatchAdded(profiles: List<ProxyEntity>)"))
        assertTrue(fragment.contains("profiles.filter { it.groupId == proxyGroup.id }"))
        assertTrue(fragment.contains("notifyItemRangeInserted(start, added.size)"))
        assertFalse(fragment.contains("if (profiles.isNotEmpty()) onMainDispatcher { reload(now = true) }"))
        assertTrue(fragment.contains("if (groupList.none { it.id == groupId })"))
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
