package io.nekohasekai.sagernet.utils

/** One user event, two preferences, one optional reload. Restoration is not an event. */
object ShareToggle {
    fun apply(previous: Boolean, requested: Boolean, started: Boolean,
              write: (Boolean) -> Unit, reload: () -> Unit): Boolean {
        if (previous == requested) return false
        write(requested)
        if (started) reload()
        return true
    }
}
