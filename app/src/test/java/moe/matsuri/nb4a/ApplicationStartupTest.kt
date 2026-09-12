package moe.matsuri.nb4a

import android.app.Application
import android.content.Context
import android.net.ConnectivityManager
import io.nekohasekai.sagernet.SagerNet
import libcore.InterfaceUpdateListener
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [28, 31], manifest = Config.NONE, application = Application::class)
class ApplicationStartupTest {
    private class Listener(private val id: Long) : InterfaceUpdateListener {
        override fun networkMonitorID() = id
        override fun updateDefaultInterface(name: String, index: Int, expensive: Boolean, constrained: Boolean) = Unit
    }

    @Test fun actualApplicationConstructsBeforeAttachThenNativeMonitorWorks() {
        // Do NOT let Robolectric create/attach SagerNet before exercising its constructor.
        val field = SagerNet::class.java.getDeclaredField("application").apply { isAccessible = true }
        val previous = field.get(null)
        field.set(null, null)
        try {
            val application = SagerNet()
            assertNull(application.baseContext)
            assertNull(field.get(null))
            val nativeField = SagerNet::class.java.getDeclaredField("nativeInterface").apply { isAccessible = true }
            val native = nativeField.get(application) as NativeInterface
            // A close without a start must also be safe before attach.
            native.closeDefaultInterfaceMonitor(Listener(99))
            val context = RuntimeEnvironment.getApplication()
            SagerNet::class.java.getDeclaredMethod("attachBaseContext", Context::class.java).apply {
                isAccessible = true
                invoke(application, context)
            }
            assertSame(context, application.baseContext)
            assertSame(application, SagerNet.application)
            val manager = application.getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager
            val shadow = shadowOf(manager)
            val before = shadow.networkCallbacks.toSet()
            val second = NativeInterface()
            val a = Listener(1)
            val b = Listener(2)
            try {
                native.startDefaultInterfaceMonitor(a)
                native.startDefaultInterfaceMonitor(a)
                second.startDefaultInterfaceMonitor(b)
                assertEquals(before.size + 2, shadow.networkCallbacks.size)
                assertTrue(org.json.JSONArray(native.networkInterfacesJSON()).length() > 0)
                native.closeDefaultInterfaceMonitor(Listener(1))
                assertEquals(before.size + 1, shadow.networkCallbacks.size)
            } finally {
                native.closeDefaultInterfaceMonitor(a)
                second.closeDefaultInterfaceMonitor(b)
            }
            assertEquals(before, shadow.networkCallbacks.toSet())
        } finally {
            field.set(null, previous)
        }
    }
}
