package io.nekohasekai.sagernet.ui.profile

import android.app.Application
import android.content.Intent
import android.os.Bundle
import android.os.Looper
import androidx.fragment.app.FragmentActivity
import androidx.preference.EditTextPreference
import androidx.preference.PreferenceFragmentCompat
import androidx.preference.SwitchPreference
import io.nekohasekai.sagernet.R
import io.nekohasekai.sagernet.RoomLifecycleRule
import io.nekohasekai.sagernet.database.DataStore
import io.nekohasekai.sagernet.database.ProxyGroup
import io.nekohasekai.sagernet.database.SagerDatabase
import io.nekohasekai.sagernet.fmt.shadowsocks.ShadowsocksBean
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.runBlocking
import moe.matsuri.nb4a.TempDatabase
import moe.matsuri.nb4a.ui.SimpleMenuPreference
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [28, 31], application = Application::class)
class GostPluginSettingsUiTest {
    @get:Rule
    val roomLifecycle = RoomLifecycleRule()

    @After
    fun resetEditorHooks() {
        ProfileSettingsActivity.resetTestHooks()
    }

    @Test fun structuredStateRoundTripsAndPreservesAdvancedUnknownOptions() {
        val raw = "mode=mwss;host=cdn.invalid;path=/gost;mux=3;insecure;nocomp;future=value\\;semi"
        val state = GostPluginUiState.fromPlugin("gost-plugin;$raw")
        assertEquals("mws", state.mode)
        assertTrue(state.tls)
        assertEquals("cdn.invalid", state.host)
        assertEquals("/gost", state.path)
        assertEquals("3", state.mux.editorText())
        assertEquals(raw, state.rawOptions)

        val saved = state.copy(mode = "ws", tls = false, host = "edge.invalid", path = "/next")
            .toPlugin("gost-plugin")
        assertEquals(
            "gost-plugin;mode=ws;host=edge.invalid;path=/next;insecure;nocomp;mux=3;future=value\\;semi",
            saved,
        )
    }

    @Test fun unsupportedModeAndMuxFlagRemainUntouchedUntilAnExplicitStructuredEdit() {
        val customRaw = "mode=grpc;host=cdn.invalid;mux;future=false"
        val custom = GostPluginUiState.fromPlugin("gost-plugin;$customRaw")
        assertEquals("grpc", custom.mode)
        assertFalse(custom.modeSupported)
        assertEquals(GostMuxValue.Flag, custom.mux)
        assertEquals("gost-plugin;$customRaw", custom.toPlugin("gost-plugin"))

        val explicit = custom.copy(mode = "ws", modeSupported = true, tls = true)
        assertEquals(
            "gost-plugin;mode=wss;host=cdn.invalid;mux;future=false",
            explicit.toPlugin("gost-plugin"),
        )
    }

    @Test fun ordinaryPluginRemainsOpaqueAndUnchanged() {
        val raw = "mode=websocket;host=cdn.invalid;path=/v;mux=8"
        val state = GostPluginUiState.fromPlugin("v2ray-plugin;$raw")
        assertFalse(state.enabled)
        assertEquals("v2ray-plugin;$raw", state.toPlugin("v2ray-plugin"))
    }

    @Test fun realActivityKeepsGostAndOtherPluginConfigsIsolatedAndMergesImmediately() {
        DataStore.editingId = 0L
        DataStore.profileCacheStore.putString("pluginName", "obfs-local")
        DataStore.profileCacheStore.putString("pluginConfig", "obfs=http;obfs-host=plain.invalid")
        val controller = Robolectric.buildActivity(ShadowsocksSettingsActivity::class.java).setup()
        controller.get().awaitInitializationForTest()
        val activity = controller.get()
        val fragment = activity.supportFragmentManager.fragments.single() as PreferenceFragmentCompat
        val plugin = fragment.findPreference<SimpleMenuPreference>("pluginName")!!
        val raw = fragment.findPreference<EditTextPreference>("pluginConfig")!!
        val host = fragment.findPreference<EditTextPreference>("gostPluginHost")!!

        plugin.callChangeListener("obfs-local")
        plugin.value = "obfs-local"
        raw.callChangeListener("obfs=http;obfs-host=plain.invalid")
        raw.text = "obfs=http;obfs-host=plain.invalid"
        plugin.callChangeListener("gost-plugin")
        plugin.value = "gost-plugin"
        raw.callChangeListener("mode=grpc;mux;future=false")
        raw.text = "mode=grpc;mux;future=false"
        host.callChangeListener("cdn.invalid")
        host.text = "cdn.invalid"
        assertEquals("mode=grpc;host=cdn.invalid;mux;future=false", raw.text)

        plugin.callChangeListener("obfs-local")
        plugin.value = "obfs-local"
        assertEquals("obfs=http;obfs-host=plain.invalid", raw.text)
        plugin.callChangeListener("gost-plugin")
        plugin.value = "gost-plugin"
        assertEquals("mode=grpc;host=cdn.invalid;mux;future=false", raw.text)
        controller.pause().stop().destroy()
    }

    @Test fun activityRecreationRestoresGostRawAndIsolatedPluginConfigsBeforeSerialize() {
        DataStore.profileCacheStore.reset()
        val controller = launchEditor()
        selectPlugin(controller.get(), "obfs-local")
        changeRaw(controller.get(), "obfs=http;obfs-host=plain.invalid")
        selectPlugin(controller.get(), "gost-plugin")
        changeRaw(controller.get(), "mode=grpc;mux;future=false")
        changeText(controller.get(), "gostPluginHost", "cdn.invalid")

        controller.recreate()
        controller.get().awaitInitializationForTest()
        assertEditorState(controller.get(), "gost-plugin", "mode=grpc;host=cdn.invalid;mux;future=false")
        selectPlugin(controller.get(), "obfs-local")
        assertEditorState(controller.get(), "obfs-local", "obfs=http;obfs-host=plain.invalid")
        selectPlugin(controller.get(), "gost-plugin")

        assertEquals(
            "gost-plugin;mode=grpc;host=cdn.invalid;mux;future=false",
            serializeProduction(controller.get()).plugin,
        )
        controller.pause().stop().destroy()
    }

    @Test fun destroyedNewEditorWithoutSavedStateStartsFreshSession() {
        DataStore.profileCacheStore.reset()
        var controller = launchEditor()
        selectPlugin(controller.get(), "gost-plugin")
        changeRaw(controller.get(), "mode=mwss;host=cdn.invalid;path=/gost;mux=3;future=value\\;semi")
        controller.pause().stop().destroy()

        controller = launchEditor()
        assertEditorState(controller.get(), "", "")
        controller.pause().stop().destroy()
    }

    @Test fun openingAnotherProfileCannotReadPreviousGostSession(): Unit = runBlocking {
        DataStore.profileCacheStore.reset()
        val group = createGroup("isolated")
        val gostId = createProfile(group.id, "gost-plugin;mode=mwss;host=a.invalid;future=a")
        val plainId = createProfile(group.id, "")

        var controller = launchExisting(gostId)
        assertEditorState(controller.get(), "gost-plugin", "mode=mwss;host=a.invalid;future=a")
        controller.pause().stop().destroy()

        controller = launchExisting(plainId)
        assertEditorState(controller.get(), "", "")
        selectPlugin(controller.get(), "gost-plugin")
        assertEditorState(controller.get(), "gost-plugin", "")
        controller.pause().stop().destroy()
    }

    @Test fun openingNewProfileCannotReadPreviousGostSession(): Unit = runBlocking {
        DataStore.profileCacheStore.reset()
        val group = createGroup("new-isolated")
        val gostId = createProfile(group.id, "gost-plugin;mode=grpc;host=a.invalid;future=a")

        var controller = launchExisting(gostId)
        assertEditorState(controller.get(), "gost-plugin", "mode=grpc;host=a.invalid;future=a")
        controller.pause().stop().destroy()

        DataStore.selectedGroup = group.id
        controller = Robolectric.buildActivity(ShadowsocksSettingsActivity::class.java).setup()
        controller.get().awaitInitializationForTest()
        assertEditorState(controller.get(), "", "")
        selectPlugin(controller.get(), "gost-plugin")
        assertEditorState(controller.get(), "gost-plugin", "")
        controller.pause().stop().destroy()
    }

    @Test fun processDeathReloadsExistingProfileFromDatabaseWithoutChangingIdentity(): Unit = runBlocking {
        DataStore.profileCacheStore.reset()
        val group = createGroup("death-existing")
        val profileId = createProfile(group.id, "gost-plugin;mode=mwss;host=persisted.invalid;future=db")
        val intent = editorIntent(profileId)
        var controller = Robolectric.buildActivity(ShadowsocksSettingsActivity::class.java, intent).setup()
        changeRaw(controller.get(), "mode=grpc;host=unsaved.invalid;future=ui")
        val savedState = Bundle()
        controller.saveInstanceState(savedState).pause().stop().destroy()

        DataStore.profileCacheStore.reset()
        controller = Robolectric.buildActivity(ShadowsocksSettingsActivity::class.java, intent)
            .create(savedState).start().resume().visible()

        assertEquals(profileId, DataStore.editingId)
        assertEquals(group.id, DataStore.editingGroup)
        assertEditorState(
            controller.get(),
            "gost-plugin",
            "mode=mwss;host=persisted.invalid;future=db",
        )
        val persisted = SagerDatabase.proxyDao.getById(profileId)!!.requireBean() as ShadowsocksBean
        assertEquals("gost-plugin;mode=mwss;host=persisted.invalid;future=db", persisted.plugin)
        controller.pause().stop().destroy()
    }

    @Test fun processDeathRebuildsNewProfileEmptyInOriginalGroup() {
        DataStore.profileCacheStore.reset()
        val group = createGroup("death-new")
        DataStore.selectedGroup = group.id
        val intent = editorIntent(0L)
        var controller = Robolectric.buildActivity(ShadowsocksSettingsActivity::class.java, intent).setup()
        selectPlugin(controller.get(), "gost-plugin")
        changeRaw(controller.get(), "mode=grpc;host=unsaved.invalid;future=ui")
        val savedState = Bundle()
        controller.saveInstanceState(savedState).pause().stop().destroy()

        DataStore.profileCacheStore.reset()
        DataStore.editingId = 99L
        controller = Robolectric.buildActivity(ShadowsocksSettingsActivity::class.java, intent)
            .create(savedState).start().resume().visible()

        assertEquals(0L, DataStore.editingId)
        assertEquals(group.id, DataStore.editingGroup)
        assertEditorState(controller.get(), "", "")
        controller.pause().stop().destroy()
    }

    @Test fun staleInitializationCannotOverwriteNewOwnerOrCommitFragment(): Unit = runBlocking {
        DataStore.profileCacheStore.reset()
        val group = createGroup("stale")
        val firstId = createProfile(group.id, "gost-plugin;mode=mwss;host=first.invalid")
        val secondId = createProfile(group.id, "gost-plugin;mode=grpc;host=second.invalid")
        val firstEntered = CompletableDeferred<Unit>()
        val releaseFirst = CompletableDeferred<Unit>()
        ProfileSettingsActivity.testEntityLoader = { profileId ->
            if (profileId == firstId) {
                firstEntered.complete(Unit)
                releaseFirst.await()
            }
            SagerDatabase.proxyDao.getById(profileId)
        }

        val first = Robolectric.buildActivity(
            ShadowsocksSettingsActivity::class.java,
            editorIntent(firstId),
        ).create().start().resume().visible()
        firstEntered.await()
        val second = launchExisting(secondId)
        assertEquals(secondId, second.get().currentEditingId())
        assertEquals(group.id, second.get().currentEditingGroup())
        val secondOwner = DataStore.profileCacheStore.getString("profileSettings.cacheSessionId")
        val secondCache = TempDatabase.profileCacheDao.all().associate {
            it.key to Pair(it.valueType, it.value.toList())
        }
        releaseFirst.complete(Unit)
        first.get().awaitInitializationForTest()

        assertEquals(secondId, DataStore.editingId)
        assertEquals(secondOwner, DataStore.profileCacheStore.getString("profileSettings.cacheSessionId"))
        assertEquals(
            secondCache,
            TempDatabase.profileCacheDao.all().associate {
                it.key to Pair(it.valueType, it.value.toList())
            },
        )
        assertTrue(first.get().supportFragmentManager.fragments.isEmpty())
        first.pause().stop().destroy()
        second.pause().stop().destroy()
    }

    @Test fun recreateWhileInitializationPendingTakesOverAndCreatesOneFragment(): Unit = runBlocking {
        DataStore.profileCacheStore.reset()
        val group = createGroup("recreate-pending")
        val profileId = createProfile(group.id, "gost-plugin;mode=mwss;host=recreated.invalid")
        val firstEntered = CompletableDeferred<Unit>()
        val releaseFirst = CompletableDeferred<Unit>()
        ProfileSettingsActivity.testEntityLoader = { id ->
            if (!firstEntered.isCompleted) {
                firstEntered.complete(Unit)
                releaseFirst.await()
            }
            SagerDatabase.proxyDao.getById(id)
        }

        val controller = Robolectric.buildActivity(
            ShadowsocksSettingsActivity::class.java,
            editorIntent(profileId),
        ).create().start().resume().visible()
        firstEntered.await()
        controller.recreate()
        releaseFirst.complete(Unit)
        controller.get().awaitInitializationForTest()

        assertEquals(profileId, controller.get().currentEditingId())
        assertEquals(group.id, controller.get().currentEditingGroup())
        assertFalse(controller.get().isFinishing)
        controller.pause().stop().destroy()
    }

    @Test fun concurrentEditorsSaveTheirOwnIdentityAndGroup(): Unit = runBlocking {
        DataStore.profileCacheStore.reset()
        val existingGroup = createGroup("save-existing")
        val newGroup = createGroup("save-new")
        val existingId = createProfile(existingGroup.id, "gost-plugin;mode=ws;host=old.invalid")
        val existing = launchExisting(existingId)

        DataStore.selectedGroup = newGroup.id
        val fresh = Robolectric.buildActivity(ShadowsocksSettingsActivity::class.java).setup()
        fresh.get().awaitInitializationForTest()

        existing.get().claimOwnershipForTest(
            mapOf(
                "pluginName" to "gost-plugin",
                "pluginConfig" to "mode=ws;host=updated.invalid",
            )
        )
        existing.get().saveAndExit()
        fresh.get().claimOwnershipForTest(
            mapOf(
                "pluginName" to "gost-plugin",
                "pluginConfig" to "mode=mwss;host=created.invalid",
            )
        )
        fresh.get().saveAndExit()

        val updated = SagerDatabase.proxyDao.getById(existingId)!!
        assertEquals(existingGroup.id, updated.groupId)
        val created = SagerDatabase.proxyDao.getAll().single { it.id != existingId }
        assertEquals(newGroup.id, created.groupId)
    }

    @Test fun directSaveAfterGostEditPersistsRealBeanInsteadOfBarePlugin(): Unit = runBlocking {
        DataStore.profileCacheStore.reset()
        val controller = launchEditor()
        selectPlugin(controller.get(), "gost-plugin")
        changeRaw(controller.get(), "mode=grpc;mux;future=false")
        changeText(controller.get(), "gostPluginHost", "cdn.invalid")

        controller.get().saveAndExit()

        val saved = SagerDatabase.proxyDao.getAll().single().requireBean() as ShadowsocksBean
        assertEquals("gost-plugin;mode=grpc;host=cdn.invalid;mux;future=false", saved.plugin)
    }

    private fun createGroup(name: String): ProxyGroup {
        val group = ProxyGroup(name = name)
        group.id = SagerDatabase.groupDao.createGroup(group)
        return group
    }

    private suspend fun createProfile(groupId: Long, plugin: String): Long {
        return io.nekohasekai.sagernet.database.ProfileManager.createProfile(
            groupId,
            ShadowsocksBean().apply {
                initializeDefaultValues()
                this.plugin = plugin
            },
        ).id
    }

    private fun editorIntent(profileId: Long) =
        Intent(RuntimeEnvironment.getApplication(), ShadowsocksSettingsActivity::class.java).apply {
            putExtra(ProfileSettingsActivity.EXTRA_PROFILE_ID, profileId)
        }

    private fun launchExisting(profileId: Long) = Robolectric.buildActivity(
        ShadowsocksSettingsActivity::class.java,
        editorIntent(profileId),
    ).setup().also { it.get().awaitInitializationForTest() }

    private fun launchEditor(): org.robolectric.android.controller.ActivityController<ShadowsocksSettingsActivity> {
        val group = createGroup("test")
        DataStore.selectedGroup = group.id
        return Robolectric.buildActivity(ShadowsocksSettingsActivity::class.java).setup().also {
            it.get().awaitInitializationForTest()
        }
    }

    private fun fragment(activity: ShadowsocksSettingsActivity): PreferenceFragmentCompat {
        activity.supportFragmentManager.executePendingTransactions()
        shadowOf(Looper.getMainLooper()).idle()
        activity.supportFragmentManager.executePendingTransactions()
        return activity.supportFragmentManager.fragments.single() as PreferenceFragmentCompat
    }

    private fun selectPlugin(activity: ShadowsocksSettingsActivity, name: String) {
        val preference = fragment(activity).findPreference<SimpleMenuPreference>("pluginName")!!
        preference.callChangeListener(name)
        preference.value = name
    }

    private fun changeRaw(activity: ShadowsocksSettingsActivity, raw: String) =
        changeText(activity, "pluginConfig", raw)

    private fun changeText(activity: ShadowsocksSettingsActivity, key: String, text: String) {
        val preference = fragment(activity).findPreference<EditTextPreference>(key)!!
        preference.callChangeListener(text)
        preference.text = text
    }

    private fun assertEditorState(activity: ShadowsocksSettingsActivity, plugin: String, raw: String) {
        val current = fragment(activity)
        assertEquals(plugin, current.findPreference<SimpleMenuPreference>("pluginName")!!.value)
        assertEquals(raw, current.findPreference<EditTextPreference>("pluginConfig")!!.text)
    }

    private fun serializeProduction(activity: ShadowsocksSettingsActivity): ShadowsocksBean {
        val bean = ShadowsocksBean()
        with(activity) { bean.serialize() }
        return bean
    }

    @Test fun preferencesAreStructuredOnlyForGostAndSurviveFragmentRecreation() {
        val controller = Robolectric.buildActivity(FragmentActivity::class.java)
        val activity = controller.get().apply { setTheme(R.style.Theme_SagerNet) }
        controller.setup()
        var fragment = Harness()
        activity.supportFragmentManager.beginTransaction().add(fragment, "settings").commitNow()

        fragment.plugin.value = "gost-plugin"
        configureGostPluginPreferences(fragment, "gost-plugin")
        assertTrue(fragment.mode.isVisible)
        assertTrue(fragment.host.isVisible)
        assertTrue(fragment.path.isVisible)
        assertTrue(fragment.tls.isVisible)
        assertTrue(fragment.mux.isVisible)
        assertTrue(fragment.raw.isVisible)
        fragment.mode.value = "mws"
        fragment.host.text = "cdn.invalid"
        fragment.path.text = "/gost"
        fragment.tls.isChecked = true
        fragment.mux.text = "3"
        fragment.raw.text = "mode=mwss;host=cdn.invalid;path=/gost;mux=3;future=value\\;semi"
        fragment.preferenceManager.sharedPreferences!!.edit()
            .putString("gostPluginMode", "mws")
            .putString("gostPluginHost", "cdn.invalid")
            .putString("gostPluginPath", "/gost")
            .putBoolean("gostPluginTls", true)
            .putString("gostPluginMux", "3")
            .putString("pluginConfig", fragment.raw.text)
            .apply()

        activity.supportFragmentManager.beginTransaction().remove(fragment).commitNow()
        fragment = Harness()
        activity.supportFragmentManager.beginTransaction().add(fragment, "settings2").commitNow()
        assertEquals("mws", fragment.mode.value)
        assertEquals("cdn.invalid", fragment.host.text)
        assertEquals("/gost", fragment.path.text)
        assertTrue(fragment.tls.isChecked)
        assertEquals("3", fragment.mux.text)
        assertTrue(fragment.raw.text!!.contains("future=value\\;semi"))

        fragment.plugin.value = "obfs-local"
        configureGostPluginPreferences(fragment, "obfs-local")
        assertFalse(fragment.mode.isVisible)
        assertFalse(fragment.host.isVisible)
        assertFalse(fragment.path.isVisible)
        assertFalse(fragment.tls.isVisible)
        assertFalse(fragment.mux.isVisible)
        assertTrue(fragment.raw.isVisible)
        assertTrue(fragment.raw.text!!.contains("future=value\\;semi"))
    }

    class Harness : PreferenceFragmentCompat() {
        lateinit var plugin: SimpleMenuPreference
        lateinit var raw: EditTextPreference
        lateinit var mode: SimpleMenuPreference
        lateinit var host: EditTextPreference
        lateinit var path: EditTextPreference
        lateinit var tls: SwitchPreference
        lateinit var mux: EditTextPreference

        override fun onCreatePreferences(savedInstanceState: Bundle?, rootKey: String?) {
            preferenceManager.sharedPreferencesName = "gost-plugin-ui-test"
            addPreferencesFromResource(R.xml.shadowsocks_preferences)
            plugin = findPreference("pluginName")!!
            raw = findPreference("pluginConfig")!!
            mode = findPreference("gostPluginMode")!!
            host = findPreference("gostPluginHost")!!
            path = findPreference("gostPluginPath")!!
            tls = findPreference("gostPluginTls")!!
            mux = findPreference("gostPluginMux")!!
        }
    }
}
