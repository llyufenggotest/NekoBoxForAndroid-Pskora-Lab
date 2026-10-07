package io.nekohasekai.sagernet

import android.content.Context
import androidx.room.Room
import androidx.room.RoomDatabase
import io.nekohasekai.sagernet.database.DataStore
import io.nekohasekai.sagernet.database.SagerDatabase
import io.nekohasekai.sagernet.database.preference.KeyValuePair
import io.nekohasekai.sagernet.database.preference.PublicDatabase
import io.nekohasekai.sagernet.database.preference.RoomPreferenceDataStore
import moe.matsuri.nb4a.TempDatabase
import org.junit.rules.ExternalResource
import org.robolectric.RuntimeEnvironment

/**
 * Robolectric reuses sandbox classes, but resets SQLite shadows between methods.
 * Close Room before that reset, then rebuild its process-lifetime caches for the
 * next Application. All schemas, generated DAOs and preference delegates stay real.
 * Reflection is confined here: production singletons need no host-test reset API.
 */
class RoomLifecycleRule : ExternalResource() {
    private val databases = mutableListOf<RoomDatabase>()

    override fun before() {
        val context = RuntimeEnvironment.getApplication()
        val app = SagerNet()
        SagerNet::class.java.getDeclaredMethod("attachBaseContext", Context::class.java).apply {
            isAccessible = true
            invoke(app, context)
        }

        val publicDb = Room.databaseBuilder(app, PublicDatabase::class.java, Key.DB_PUBLIC)
            .setJournalMode(RoomDatabase.JournalMode.TRUNCATE)
            .allowMainThreadQueries().setQueryExecutor { it.run() }.build()
        val profileDb = Room.databaseBuilder(app, SagerDatabase::class.java, Key.DB_PROFILE)
            .setJournalMode(RoomDatabase.JournalMode.TRUNCATE)
            .allowMainThreadQueries().setQueryExecutor { it.run() }.build()
        val tempDb = Room.inMemoryDatabaseBuilder(app, TempDatabase::class.java)
            .allowMainThreadQueries().setQueryExecutor { it.run() }.build()
        databases.addAll(listOf(publicDb, profileDb, tempDb))
        installInstance(PublicDatabase::class.java, publicDb)
        installInstance(SagerDatabase::class.java, profileDb)
        installInstance(TempDatabase::class.java, tempDb)

        // PreferenceProxy delegates capture the store, so replace its DAO rather
        // than replacing DataStore.configurationStore/profileCacheStore themselves.
        installDao(DataStore.configurationStore, publicDb.keyValuePairDao())
        installDao(DataStore.profileCacheStore, tempDb.profileCacheDao())
        publicDb.clearAllTables()
        profileDb.clearAllTables()
        tempDb.clearAllTables()
    }

    override fun after() {
        try {
            databases.asReversed().forEach { it.close() }
        } finally {
            databases.clear()
        }
    }

    private fun installInstance(owner: Class<*>, database: RoomDatabase) {
        val delegate = owner.getDeclaredField("instance\$delegate").apply {
            isAccessible = true
        }.get(null) as Lazy<*>
        // Keep the Lazy wrapper used by production getters; only replace its
        // cached value. Never attempt to revive a consumed/null lazy initializer.
        delegate.javaClass.getDeclaredField("_value").apply {
            isAccessible = true
            set(delegate, database)
        }
    }

    private fun installDao(store: RoomPreferenceDataStore, dao: KeyValuePair.Dao) {
        RoomPreferenceDataStore::class.java.getDeclaredField("kvPairDao").apply {
            isAccessible = true
            set(store, dao)
        }
    }
}
