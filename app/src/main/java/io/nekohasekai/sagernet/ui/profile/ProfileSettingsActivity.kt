package io.nekohasekai.sagernet.ui.profile

import android.annotation.SuppressLint
import android.content.DialogInterface
import android.content.Intent
import android.os.Build
import android.os.Bundle
import android.os.Parcelable
import android.view.Menu
import android.view.MenuItem
import android.view.View
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.Toast
import androidx.activity.result.component1
import androidx.activity.result.component2
import androidx.activity.result.contract.ActivityResultContracts
import androidx.annotation.LayoutRes
import androidx.appcompat.app.AlertDialog
import androidx.lifecycle.lifecycleScope
import androidx.core.content.pm.ShortcutInfoCompat
import androidx.core.content.pm.ShortcutManagerCompat
import androidx.core.graphics.drawable.IconCompat
import androidx.core.view.ViewCompat
import androidx.core.view.isVisible
import androidx.preference.EditTextPreference
import androidx.preference.Preference
import androidx.preference.PreferenceDataStore
import androidx.preference.PreferenceFragmentCompat
import com.github.shadowsocks.plugin.Empty
import com.github.shadowsocks.plugin.fragment.AlertDialogFragment
import com.google.android.material.dialog.MaterialAlertDialogBuilder
import io.nekohasekai.sagernet.*
import io.nekohasekai.sagernet.database.DataStore
import io.nekohasekai.sagernet.database.GroupManager
import io.nekohasekai.sagernet.database.ProfileManager
import io.nekohasekai.sagernet.database.ProxyEntity
import io.nekohasekai.sagernet.database.SagerDatabase
import io.nekohasekai.sagernet.database.preference.OnPreferenceDataStoreChangeListener
import io.nekohasekai.sagernet.databinding.LayoutGroupItemBinding
import io.nekohasekai.sagernet.fmt.AbstractBean
import io.nekohasekai.sagernet.ktx.*
import io.nekohasekai.sagernet.ui.ThemedActivity
import io.nekohasekai.sagernet.widget.ListListener
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import kotlinx.parcelize.Parcelize
import java.util.UUID
import kotlin.coroutines.coroutineContext
import kotlin.properties.Delegates

@Suppress("UNCHECKED_CAST")
abstract class ProfileSettingsActivity<T : AbstractBean>(
    @LayoutRes resId: Int = R.layout.layout_config_settings,
) : ThemedActivity(resId), OnPreferenceDataStoreChangeListener {

    class UnsavedChangesDialogFragment : AlertDialogFragment<Empty, Empty>() {
        override fun AlertDialog.Builder.prepare(listener: DialogInterface.OnClickListener) {
            setTitle(R.string.unsaved_changes_prompt)
            setPositiveButton(R.string.yes) { _, _ ->
                (requireActivity() as ProfileSettingsActivity<*>).let { activity ->
                    activity.lifecycleScope.launch(Dispatchers.Default) {
                        activity.saveAndExit()
                    }
                }
            }
            setNegativeButton(R.string.no) { _, _ ->
                requireActivity().finish()
            }
            setNeutralButton(android.R.string.cancel, null)
        }
    }

    @Parcelize
    data class ProfileIdArg(val profileId: Long, val groupId: Long) : Parcelable
    class DeleteConfirmationDialogFragment : AlertDialogFragment<ProfileIdArg, Empty>() {
        override fun AlertDialog.Builder.prepare(listener: DialogInterface.OnClickListener) {
            setTitle(R.string.delete_confirm_prompt)
            setPositiveButton(R.string.yes) { _, _ ->
                val activity = requireActivity() as ProfileSettingsActivity<*>
                activity.lifecycleScope.launch(Dispatchers.Default) {
                    ProfileManager.deleteProfile(arg.groupId, arg.profileId)
                }
                activity.finish()
            }
            setNegativeButton(R.string.no, null)
        }
    }

    companion object {
        const val EXTRA_PROFILE_ID = "id"
        const val EXTRA_IS_SUBSCRIPTION = "sub"

        private const val STATE_EDITING_ID = "profileSettings.editingId"
        private const val STATE_EDITING_GROUP = "profileSettings.editingGroup"
        private const val STATE_IS_SUBSCRIPTION = "profileSettings.isSubscription"
        private const val STATE_SESSION_ID = "profileSettings.sessionId"
        private const val CACHE_SESSION_ID = "profileSettings.cacheSessionId"
        private const val CACHE_SESSION_PROFILE_ID = "profileSettings.cacheSessionProfileId"
        private const val CACHE_INITIALIZED = "profileSettings.cacheInitialized"

        @Volatile
        internal var testEntityLoader: (suspend (Long) -> ProxyEntity?)? = null

        internal fun resetTestHooks() {
            testEntityLoader = null
        }
    }

    abstract fun createEntity(): T
    abstract fun T.init()
    abstract fun T.serialize()

    private var editingId = 0L
    private var editingGroup = 0L
    private lateinit var editingSessionId: String
    private var restoredEditorCache = false
    private var loadedProxyEntity: ProxyEntity? = null
    private var initializationJob: Job? = null
    private var destroyed = false
    val proxyEntity get() = loadedProxyEntity
    protected var isSubscription by Delegates.notNull<Boolean>()

    private fun restoreEditorIdentity(savedInstanceState: Bundle?) {
        editingId = savedInstanceState?.getLong(STATE_EDITING_ID)
            ?: intent.getLongExtra(EXTRA_PROFILE_ID, 0L)
        isSubscription = savedInstanceState?.getBoolean(STATE_IS_SUBSCRIPTION)
            ?: intent.getBooleanExtra(EXTRA_IS_SUBSCRIPTION, false)
        editingGroup = savedInstanceState?.getLong(STATE_EDITING_GROUP) ?: 0L
        editingSessionId = savedInstanceState?.getString(STATE_SESSION_ID)
            ?: UUID.randomUUID().toString()

        val cachedSession = DataStore.profileCacheStore.getString(CACHE_SESSION_ID)
        val cachedProfile = DataStore.profileCacheStore.getLong(CACHE_SESSION_PROFILE_ID)
        val cacheInitialized = DataStore.profileCacheStore.getBoolean(CACHE_INITIALIZED) == true
        restoredEditorCache = savedInstanceState != null && cacheInitialized &&
            cachedSession == editingSessionId && cachedProfile == editingId
        if (!restoredEditorCache) {
            DataStore.profileCacheStore.reset()
            claimCacheOwner(initialized = false)
        } else {
            claimCacheOwner(initialized = true)
        }
    }

    private fun ownsCache(): Boolean {
        return !destroyed &&
            DataStore.profileCacheStore.getString(CACHE_SESSION_ID) == editingSessionId &&
            DataStore.profileCacheStore.getLong(CACHE_SESSION_PROFILE_ID) == editingId
    }

    private fun claimCacheOwner(initialized: Boolean) {
        if (destroyed) return
        DataStore.profileCacheStore.putString(CACHE_SESSION_ID, editingSessionId)
        DataStore.profileCacheStore.putLong(CACHE_SESSION_PROFILE_ID, editingId)
        DataStore.profileCacheStore.putBoolean(CACHE_INITIALIZED, initialized)
        publishEditorIdentity()
    }

    private fun publishEditorIdentity() {
        if (!ownsCache()) return
        DataStore.editingId = editingId
        if (editingGroup != 0L) DataStore.editingGroup = editingGroup
    }

    private suspend fun verifySessionOwner(): Boolean {
        coroutineContext.ensureActive()
        return ownsCache()
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        restoreEditorIdentity(savedInstanceState)
        super.onCreate(savedInstanceState)
        setSupportActionBar(findViewById(R.id.toolbar))
        supportActionBar?.apply {
            setTitle(R.string.profile_config)
            setDisplayHomeAsUpEnabled(true)
            setHomeAsUpIndicator(R.drawable.ic_navigation_close)
        }

        if (editingId != 0L && restoredEditorCache) {
            loadedProxyEntity = SagerDatabase.proxyDao.getById(editingId)
        }
        if (savedInstanceState == null || !restoredEditorCache) {
            initializationJob = lifecycleScope.launch {
                initializeEditorFromSource()
            }
        } else if (supportFragmentManager.findFragmentById(R.id.settings) == null) {
            // A completed cache can restore preferences, but the fragment still needs an owner.
            initializationJob = lifecycleScope.launch {
                if (!verifySessionOwner()) return@launch
                if (!ownsCache() || isFinishing || isDestroyed) return@launch
                if (supportFragmentManager.findFragmentById(R.id.settings) == null) {
                    supportFragmentManager.beginTransaction()
                        .replace(R.id.settings, MyPreferenceFragmentCompat())
                        .commitNow()
                }
            }
        }
    }

    private suspend fun loadEntity(profileId: Long): ProxyEntity? {
        return testEntityLoader?.invoke(profileId) ?: SagerDatabase.proxyDao.getById(profileId)
    }

    private suspend fun initializeEditorFromSource() {
        if (!verifySessionOwner()) return
        if (editingId == 0L) {
            val targetGroup = if (editingGroup != 0L) editingGroup else DataStore.selectedGroupForImport()
            if (!verifySessionOwner()) return
            editingGroup = targetGroup
            loadedProxyEntity = null
            publishEditorIdentity()
            val editorBean = createEntity().applyDefaultValues()
            if (!verifySessionOwner()) return
            editorBean.init()
            finishInitialization()
            return
        }

        val entity = loadEntity(editingId)
        if (!verifySessionOwner()) return
        if (entity == null) {
            withContext(Dispatchers.Main.immediate) {
                if (ownsCache() && !isFinishing && !isDestroyed) finish()
            }
            return
        }
        editingGroup = entity.groupId
        loadedProxyEntity = entity
        publishEditorIdentity()
        val editorBean = entity.requireBean() as T
        if (!verifySessionOwner()) return
        editorBean.init()
        finishInitialization()
    }

    private suspend fun finishInitialization() {
        if (!verifySessionOwner()) return
        DataStore.profileCacheStore.putBoolean(CACHE_INITIALIZED, true)
        if (!verifySessionOwner()) return
        if (!ownsCache() || isFinishing || isDestroyed) return
        if (supportFragmentManager.findFragmentById(R.id.settings) == null) {
            supportFragmentManager.beginTransaction()
                .replace(R.id.settings, MyPreferenceFragmentCompat())
                .commitNow()
        }
    }

    override fun onSaveInstanceState(outState: Bundle) {
        outState.putLong(STATE_EDITING_ID, editingId)
        outState.putLong(STATE_EDITING_GROUP, editingGroup)
        outState.putBoolean(STATE_IS_SUBSCRIPTION, isSubscription)
        outState.putString(STATE_SESSION_ID, editingSessionId)
        super.onSaveInstanceState(outState)
    }

    open suspend fun saveAndExit() {
        if (!verifySessionOwner()) return

        val targetId = editingId
        val targetGroup = editingGroup
        val serializedBean = createEntity().apply { serialize() }
        if (targetId == 0L) {
            ProfileManager.createProfile(targetGroup, serializedBean)
        } else {
            val entity = loadedProxyEntity ?: loadEntity(targetId)
            if (entity == null || entity.id != targetId) {
                withContext(Dispatchers.Main.immediate) { finish() }
                return
            }
            loadedProxyEntity = entity
            if (entity.id == DataStore.selectedProxy) {
                SagerNet.stopService()
            }
            entity.putBean(serializedBean)
            ProfileManager.updateProfile(entity)
        }
        withContext(Dispatchers.Main.immediate) { finish() }
    }

    internal fun awaitInitializationForTest() {
        kotlinx.coroutines.runBlocking {
            initializationJob?.join()
            withContext(Dispatchers.Main.immediate) { }
        }
    }

    internal suspend fun claimOwnershipForTest(values: Map<String, String>) {
        claimCacheOwner(initialized = true)
        if (editingId != 0L) {
            loadedProxyEntity = loadEntity(editingId)
        }
        values.forEach { (key, value) -> DataStore.profileCacheStore.putString(key, value) }
    }

    internal fun currentEditingId() = editingId
    internal fun currentEditingGroup() = editingGroup
    internal fun setEditingGroup(groupId: Long) {
        editingGroup = groupId
        if (ownsCache()) DataStore.editingGroup = groupId
    }

    val child by lazy { supportFragmentManager.findFragmentById(R.id.settings) as MyPreferenceFragmentCompat }

    override fun onCreateOptionsMenu(menu: Menu): Boolean {
        menuInflater.inflate(R.menu.profile_config_menu, menu)
        menu.findItem(R.id.action_move)?.apply {
            if (editingId != 0L // not new profile
                && SagerDatabase.groupDao.getById(editingGroup)?.type == GroupType.BASIC // not in subscription group
                && SagerDatabase.groupDao.allGroups()
                    .filter { it.type == GroupType.BASIC }.size > 1 // have other basic group
            ) isVisible = true
        }
        menu.findItem(R.id.action_create_shortcut)?.apply {
            if (Build.VERSION.SDK_INT >= 26 && editingId != 0L) {
                isVisible = true // not new profile
            }
        }
        // shared menu item
        menu.findItem(R.id.action_custom_outbound_json)?.isVisible = true
        menu.findItem(R.id.action_custom_config_json)?.isVisible = true
        return true
    }

    override fun onOptionsItemSelected(item: MenuItem) = child.onOptionsItemSelected(item)

    override fun onBackPressed() {
        if (DataStore.dirty) UnsavedChangesDialogFragment().apply { key() }
            .show(supportFragmentManager, null) else super.onBackPressed()
    }

    override fun onSupportNavigateUp(): Boolean {
        if (!super.onSupportNavigateUp()) finish()
        return true
    }

    override fun onDestroy() {
        DataStore.profileCacheStore.unregisterChangeListener(this)
        destroyed = true
        initializationJob?.cancel()
        initializationJob = null
        super.onDestroy()
    }

    override fun onPreferenceDataStoreChanged(store: PreferenceDataStore, key: String) {
        if (key != Key.PROFILE_DIRTY) {
            DataStore.dirty = true
        }
    }

    abstract fun PreferenceFragmentCompat.createPreferences(
        savedInstanceState: Bundle?,
        rootKey: String?,
    )

    open fun PreferenceFragmentCompat.viewCreated(view: View, savedInstanceState: Bundle?) {
    }

    open fun PreferenceFragmentCompat.displayPreferenceDialog(preference: Preference): Boolean {
        return false
    }

    class MyPreferenceFragmentCompat : PreferenceFragmentCompat() {

        var activity: ProfileSettingsActivity<*>? = null

        override fun onCreatePreferences(savedInstanceState: Bundle?, rootKey: String?) {
            preferenceManager.preferenceDataStore = DataStore.profileCacheStore
            try {
                activity = (requireActivity() as ProfileSettingsActivity<*>).apply {
                    createPreferences(savedInstanceState, rootKey)
                }
            } catch (e: Exception) {
                Toast.makeText(
                    SagerNet.application,
                    "Error on createPreferences, please try again.",
                    Toast.LENGTH_SHORT
                ).show()
                Logs.e(e)
            }
        }

        override fun onViewCreated(view: View, savedInstanceState: Bundle?) {
            super.onViewCreated(view, savedInstanceState)

            ViewCompat.setOnApplyWindowInsetsListener(listView, ListListener)

            activity?.apply {
                viewCreated(view, savedInstanceState)
                DataStore.dirty = false
                DataStore.profileCacheStore.registerChangeListener(this)
            }
        }

        var callbackCustom: ((String) -> Unit)? = null
        var callbackCustomOutbound: ((String) -> Unit)? = null

        val resultCallbackCustom = registerForActivityResult(
            ActivityResultContracts.StartActivityForResult()
        ) { (_, _) ->
            callbackCustom?.let { it(DataStore.serverCustom) }
        }

        val resultCallbackCustomOutbound = registerForActivityResult(
            ActivityResultContracts.StartActivityForResult()
        ) { (_, _) ->
            callbackCustomOutbound?.let { it(DataStore.serverCustomOutbound) }
        }

        @SuppressLint("CheckResult")
        override fun onOptionsItemSelected(item: MenuItem) = when (item.itemId) {
            R.id.action_delete -> {
                val activity = requireActivity() as ProfileSettingsActivity<*>
                if (activity.currentEditingId() == 0L) {
                    activity.finish()
                } else {
                    DeleteConfirmationDialogFragment().apply {
                        arg(
                            ProfileIdArg(
                                activity.currentEditingId(), activity.currentEditingGroup()
                            )
                        )
                        key()
                    }.show(parentFragmentManager, null)
                }
                true
            }

            R.id.action_apply -> {
                activity?.let { editor ->
                    editor.lifecycleScope.launch(Dispatchers.Default) {
                        editor.saveAndExit()
                    }
                }
                true
            }

            R.id.action_custom_outbound_json -> {
                activity?.proxyEntity?.apply {
                    val bean = requireBean()
                    DataStore.serverCustomOutbound = bean.customOutboundJson
                    callbackCustomOutbound = { bean.customOutboundJson = it }
                    resultCallbackCustomOutbound.launch(
                        Intent(
                            requireContext(),
                            ConfigEditActivity::class.java
                        ).apply {
                            putExtra("key", Key.SERVER_CUSTOM_OUTBOUND)
                        })
                }
                true
            }

            R.id.action_custom_config_json -> {
                activity?.proxyEntity?.apply {
                    val bean = requireBean()
                    DataStore.serverCustom = bean.customConfigJson
                    callbackCustom = { bean.customConfigJson = it }
                    resultCallbackCustom.launch(
                        Intent(
                            requireContext(),
                            ConfigEditActivity::class.java
                        ).apply {
                            putExtra("key", Key.SERVER_CUSTOM)
                        })
                }
                true
            }

            R.id.action_create_shortcut -> {
                val activity = requireActivity() as ProfileSettingsActivity<*>
                val ent = activity.proxyEntity!!
                val shortcut = ShortcutInfoCompat.Builder(activity, "shortcut-profile-${ent.id}")
                    .setShortLabel(ent.displayName())
                    .setLongLabel(ent.displayName())
                    .setIcon(
                        IconCompat.createWithResource(
                            activity, R.drawable.ic_qu_shadowsocks_launcher
                        )
                    ).setIntent(Intent(
                        context, QuickToggleShortcut::class.java
                    ).apply {
                        action = Intent.ACTION_MAIN
                        putExtra("profile", ent.id)
                    }).build()
                ShortcutManagerCompat.requestPinShortcut(activity, shortcut, null)
            }

            R.id.action_move -> {
                val activity = requireActivity() as ProfileSettingsActivity<*>
                val view = LinearLayout(context).apply {
                    val ent = activity.proxyEntity!!
                    orientation = LinearLayout.VERTICAL

                    SagerDatabase.groupDao.allGroups()
                        .filter { it.type == GroupType.BASIC && it.id != ent.groupId }
                        .forEach { group ->
                            LayoutGroupItemBinding.inflate(layoutInflater, this, true).apply {
                                edit.isVisible = false
                                options.isVisible = false
                                groupName.text = group.displayName()
                                groupUpdate.text = getString(R.string.move)
                                groupUpdate.setOnClickListener {
                                    activity.lifecycleScope.launch(Dispatchers.Default) {
                                        val oldGroupId = ent.groupId
                                        val newGroupId = group.id
                                        ent.groupId = newGroupId
                                        ProfileManager.updateProfile(ent)
                                        GroupManager.postUpdate(oldGroupId) // reload
                                        GroupManager.postUpdate(newGroupId)
                                        activity.setEditingGroup(newGroupId) // post switch animation
                                        withContext(Dispatchers.Main.immediate) {
                                            activity.finish()
                                        }
                                    }
                                }
                            }
                        }
                }
                val scrollView = ScrollView(context).apply {
                    addView(view)
                }
                MaterialAlertDialogBuilder(activity).setView(scrollView).show()
                true
            }

            else -> false
        }

        override fun onDisplayPreferenceDialog(preference: Preference) {
            activity?.apply {
                if (displayPreferenceDialog(preference)) return
            }
            super.onDisplayPreferenceDialog(preference)
        }

    }

    object PasswordSummaryProvider : Preference.SummaryProvider<EditTextPreference> {

        override fun provideSummary(preference: EditTextPreference): CharSequence {
            val text = preference.text
            return if (text.isNullOrBlank()) {
                preference.context.getString(androidx.preference.R.string.not_set)
            } else {
                "\u2022".repeat(text.length)
            }
        }

    }

}