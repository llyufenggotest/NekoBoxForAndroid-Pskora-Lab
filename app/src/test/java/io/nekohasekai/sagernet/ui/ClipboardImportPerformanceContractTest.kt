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
        val configurationAdapter = fragment.substringAfter("inner class ConfigurationAdapter")
            .substringBefore("val profileAccess = Mutex()")
        assertTrue(configurationAdapter.contains("override suspend fun onBatchAdded(profiles: List<ProxyEntity>)"))
        assertTrue(configurationAdapter.contains("profiles.none { it.groupId == proxyGroup.id }"))
        assertTrue(configurationAdapter.contains("reloadProfiles()"))
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

    @Test
    fun clipboardSubscriptionNameIsAppliedInsideTheEditor() {
        val fragment = source("io/nekohasekai/sagernet/ui/ConfigurationFragment.kt")
        val settings = source("io/nekohasekai/sagernet/ui/GroupSettingsActivity.kt")
        assertTrue(fragment.contains("RawUpdater.fetchSubscriptionName(subscriptionLink)"))
        assertTrue(fragment.contains("EXTRA_GROUP_NAME, guessed"))
        assertTrue(settings.contains("fillSubscriptionName(DataStore.subscriptionLink, groupName)"))
        assertTrue(settings.contains("groupName.text = remoteName"))
    }

    @Test
    fun groupFileImportUsesTheGroupEditorEntryPoint() {
        val fragment = source("io/nekohasekai/sagernet/ui/ConfigurationFragment.kt")
        val settings = source("io/nekohasekai/sagernet/ui/GroupSettingsActivity.kt")
        assertTrue(fragment.contains("EXTRA_FROM_FILE"))
        assertTrue(settings.contains("ActivityResultContracts.GetContent"))
        assertTrue(settings.contains("RawUpdater.parseRaw"))
        assertTrue(settings.contains("ProfileManager.createProfiles"))
        assertTrue(settings.contains("groupFilePicker"))
        assertTrue(settings.contains("DataStore.groupName = pendingFileName"))
        assertTrue(settings.contains("pendingFileProxies.isNotEmpty()"))
        assertTrue(settings.contains("editingId == 0L && DataStore.groupType == GroupType.BASIC"))
        assertTrue(settings.contains("selectedType != GroupType.BASIC"))
    }

    @Test
    fun preferredConnectionRefreshesDynamicSourcesBeforeVpnStart() {
        val main = source("io/nekohasekai/sagernet/ui/MainActivity.kt")
        val store = source("io/nekohasekai/sagernet/database/PreferredGroupStore.kt")
        assertTrue(main.contains("PreferredGroupStore.syncSources"))
        assertTrue(main.contains("connect.launch(null)"))
        assertTrue(store.contains("GroupUpdater.executeUpdate"))
        assertTrue(store.contains("PreferredGroupResolver"))
    }
}
