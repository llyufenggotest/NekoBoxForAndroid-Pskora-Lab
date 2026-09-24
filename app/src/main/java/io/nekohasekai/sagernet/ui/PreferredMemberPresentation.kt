package io.nekohasekai.sagernet.ui

/** Exactly the source profile's persisted test state; never a group or live dashboard latency. */
internal fun preferredMemberLatency(status: Int, ping: Int): String = when {
    status == 1 && ping >= 0 -> "$ping ms"
    status == 2 || status == 3 -> "测试失败"
    else -> "未测试"
}

/** Manual measurements are independent of native automatic history. A newer attempt owns its ID. */
internal data class PreferredAutoSample(val sampleTime: Long, val ping: Int)
internal enum class PreferredHealthTone { HEALTHY, UNHEALTHY, NEUTRAL }

internal class PreferredTestResults(
    private val clock: () -> Long = System::currentTimeMillis,
    private var manualPriorityMillis: () -> Long = { 300_000L },
) {
    private data class Result(val ticket: Long, val kind: String, val status: Int, val ping: Int,
        val sampleTime: Long, val session: String = "manual-process")
    private var autoSession = ""
    private var automatic = emptyMap<Long, PreferredAutoSample>()
    private val priorityById = mutableMapOf<Long, Long>()
    /** Full exact-ID snapshot: disappearance means unknown, never an invented failed/0ms test.
     * Pending manual attempts always own their ID. A completed manual result owns it for one
     * configured automatic interval and also rejects automatic batches older than completion.
     * Thereafter a genuinely newer periodic sample may update the card dynamically.
     */
    @Synchronized fun replaceAutomatic(session: String, samples: Map<Long, PreferredAutoSample>) {
        autoSession = session
        automatic = if (session.isBlank()) emptyMap() else samples.filter { (id, sample) ->
            id > 0 && sample.sampleTime > 0 && sample.ping >= 0
        }.toMap()
    }
    @Synchronized fun setManualPriorityInterval(intervalMillis: Long) {
        require(intervalMillis > 0)
        manualPriorityMillis = { intervalMillis }
    }
    private val manualSession = java.util.UUID.randomUUID().toString()
    private val values = mutableMapOf<Long, Result>()
    private var sequence = 0L
    @Synchronized fun begin(id: Long, kind: String): Long {
        val ticket = ++sequence
        values[id] = Result(ticket, kind, 0, 0, clock(), manualSession)
        return ticket
    }
    @Synchronized fun complete(id: Long, ticket: Long, status: Int, ping: Int): Boolean {
        val previous = values[id] ?: return false
        if (previous.ticket != ticket) return false
        val completedAt = clock()
        values[id] = previous.copy(status = status, ping = ping, sampleTime = completedAt)
        priorityById[id] = completedAt + manualPriorityMillis().coerceAtLeast(1L)
        return true
    }
    @Synchronized fun finishPending(tickets: Map<Long, Long>) {
        tickets.forEach { (id, ticket) ->
            values[id]?.takeIf { it.ticket == ticket && it.status == 0 }?.let { values.remove(id) }
        }
    }
    private fun manualOwns(id: Long, result: Result): Boolean {
        if (result.status == 0) return true
        val automaticSample = automatic[id] ?: return true
        return automaticSample.sampleTime <= result.sampleTime || clock() < (priorityById[id] ?: result.sampleTime)
    }
    @Synchronized fun label(id: Long): String? {
        val manual = values[id]?.takeIf { manualOwns(id, it) }
        if (manual != null) {
            return manual.takeIf { it.status == 1 && it.ping >= 0 }?.let { "${it.ping} ms" }
        }
        return automatic[id]?.let { "${it.ping} ms" }
    }

    @Synchronized fun numericLabel(id: Long, persistedStatus: Int, persistedPing: Int): String =
        effectiveLatency(id, persistedStatus, persistedPing)?.let { "$it ms" }.orEmpty()

    /** Only a currently effective successful sample participates in latency ordering. */
    private fun effectiveLatency(id: Long, persistedStatus: Int, persistedPing: Int): Int? {
        val manual = values[id]?.takeIf { manualOwns(id, it) }
        if (manual != null) return manual.ping.takeIf { manual.status == 1 && it >= 0 }
        return automatic[id]?.ping ?: persistedPing.takeIf { persistedStatus == 1 && it >= 0 }
    }
    @Synchronized fun healthyLatency(id: Long, persistedStatus: Int, persistedPing: Int): Int? =
        effectiveLatency(id, persistedStatus, persistedPing)
    @Synchronized fun latencySnapshot(
        persisted: Map<Long, Pair<Int, Int>>,
    ): Map<Long, Int?> = persisted.mapValues { (id, value) -> effectiveLatency(id, value.first, value.second) }

    @Synchronized fun tone(id: Long, persistedStatus: Int): PreferredHealthTone =
        values[id]?.takeIf { manualOwns(id, it) }?.let {
            when (it.status) {
                1 -> PreferredHealthTone.HEALTHY
                2, 3 -> PreferredHealthTone.UNHEALTHY
                else -> PreferredHealthTone.NEUTRAL
            }
        } ?: if (automatic.containsKey(id)) PreferredHealthTone.HEALTHY else when (persistedStatus) {
            1 -> PreferredHealthTone.HEALTHY
            2, 3 -> PreferredHealthTone.UNHEALTHY
            else -> PreferredHealthTone.NEUTRAL
        }
}

internal val preferredTestResults = PreferredTestResults()

/** Kotlin's stable sort preserves the DB/reference order for ties and every non-success state. */
internal fun <T> preferredMembersByLatency(
    rows: List<T>,
    id: (T) -> Long,
    persistedStatus: (T) -> Int,
    persistedPing: (T) -> Int,
    results: PreferredTestResults = preferredTestResults,
): List<T> {
    val latencies = results.latencySnapshot(rows.associate { item ->
        id(item) to (persistedStatus(item) to persistedPing(item))
    })
    return rows.sortedWith(compareBy<T> { latencies[id(it)] == null }
        .thenBy { latencies[id(it)] ?: Int.MAX_VALUE })
}
