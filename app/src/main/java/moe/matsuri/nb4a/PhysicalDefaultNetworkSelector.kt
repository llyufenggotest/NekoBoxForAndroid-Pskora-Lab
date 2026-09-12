package moe.matsuri.nb4a

internal object PhysicalDefaultNetworkSelector {
    fun <T> select(
        active: T?,
        callback: T?,
        underlying: List<T>,
        usable: (T) -> Boolean
    ): T? = when {
        active != null && usable(active) -> active
        active != null -> underlying.firstOrNull(usable) ?: callback?.takeIf(usable)
        callback != null && usable(callback) -> callback
        else -> null
    }
}
