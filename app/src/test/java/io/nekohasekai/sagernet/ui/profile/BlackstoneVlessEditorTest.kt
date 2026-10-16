package io.nekohasekai.sagernet.ui.profile

import android.app.Application
import io.nekohasekai.sagernet.RoomLifecycleRule
import io.nekohasekai.sagernet.database.DataStore
import io.nekohasekai.sagernet.fmt.KryoConverters
import io.nekohasekai.sagernet.fmt.v2ray.StandardV2RayBean
import io.nekohasekai.sagernet.fmt.v2ray.VMessBean
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/** Exercise the production cache callbacks used by edit/save, without launching a service. */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [28], manifest = Config.NONE, application = Application::class,
    shadows = [BlackstoneVlessEditorTest.LoggingOnlyShadow::class])
class BlackstoneVlessEditorTest {
    // JNI logging is not part of the editor contract. No protocol/native result is mocked.
    @org.robolectric.annotation.Implements(value = libcore.Libcore::class)
    class LoggingOnlyShadow {
        companion object {
            @JvmStatic @org.robolectric.annotation.Implementation
            fun __staticInitializer__() = Unit
            @JvmStatic @org.robolectric.annotation.Implementation
            fun nekoLogPrintln(message: String) = Unit
        }
    }
    private class Editor : StandardV2RaySettingsActivity() {
        override fun createEntity() = VMessBean().apply { alterId = -1; initializeDefaultValues() }
        fun load(bean: StandardV2RayBean) { bean.init() }
        fun save() = createEntity().apply { serialize() }
    }

    @get:org.junit.Rule val roomLifecycle = RoomLifecycleRule()

    @Test fun unrelatedEditAndRecreatedEditorPreserveHiddenOptions() {
        val imported = VMessBean().apply {
            alterId = -1; uuid = "12345678-1234-1234-1234-123456789abc#blackstone"
            blackstoneE = false; blackstoneH = "fixture-h"; initializeDefaultValues()
        }
        Editor().load(imported)
        DataStore.profileCacheStore.putString("name", "edited")
        val restored = KryoConverters.vmessDeserialize(KryoConverters.serialize(Editor().save()))
        assertEquals("edited", restored.name)
        assertEquals(false, restored.blackstoneE)
        assertEquals("fixture-h", restored.blackstoneH)
    }

    @Test fun nextOrdinaryProfileClearsHiddenCache() {
        Editor().load(VMessBean().apply {
            alterId = -1; blackstoneE = true; blackstoneH = "fixture-h"; initializeDefaultValues()
        })
        Editor().load(VMessBean().apply { alterId = -1; initializeDefaultValues() })
        val saved = Editor().save()
        assertNull(saved.blackstoneE); assertNull(saved.blackstoneH)
    }
}
