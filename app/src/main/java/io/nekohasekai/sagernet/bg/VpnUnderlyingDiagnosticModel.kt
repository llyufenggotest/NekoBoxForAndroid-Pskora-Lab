package io.nekohasekai.sagernet.bg

/** Pure state model for metadata-only VPN underlying diagnostics. */
internal class VpnUnderlyingDiagnosticModel {
    enum class Result(val code: Int) { TRUE(1), FALSE(2), EXCEPTION(3), NOT_APPLICABLE(4) }

    data class Event(
        val sequence: Long,
        val kind: Int,
        val requested: Boolean,
        val offline: Boolean,
        val argumentCode: Int,
        val requestedHash: Long,
        val retainedHash: Long,
        val resultCode: Int,
        val exceptionClass: Int,
        val threadHash: Long,
        val callbackCode: Int = 0,
        val activeHash: Long = 0,
        val selectedHash: Long = 0,
        val declaredUnderlyingHash: Long = 0,
        val activeIsVpn: Boolean = false,
        val isDefault: Boolean = false,
        val validated: Boolean = false,
        val duplicate: Boolean = false,
    ) {
        fun render(): String = "VPNUNDERTRACE schema=2 seq=$sequence kind=$kind" +
            " requested=${requested.bit()} offline=${offline.bit()} arg=$argumentCode" +
            " request=$requestedHash retained=$retainedHash result=$resultCode exception=$exceptionClass" +
            " thread=$threadHash callback=$callbackCode active=$activeHash selected=$selectedHash" +
            " underlying=$declaredUnderlyingHash vpn=${activeIsVpn.bit()} default=${isDefault.bit()}" +
            " validated=${validated.bit()} duplicate=${duplicate.bit()}"
    }

    private var sequence = 0L
    private var retainedHash = 0L
    private var lastCallbackSignature: List<Any>? = null

    @Synchronized
    fun recordSetRequest(
        requestedHash: Long,
        result: Result,
        threadHash: Long,
        argumentCode: Int = 3,
        exceptionClass: Int = 0,
    ): Event {
        retainedHash = requestedHash
        return Event(++sequence, 1, true, false, argumentCode, requestedHash, retainedHash,
            result.code, exceptionClass, threadHash)
    }

    /** Mirrors the audited current behavior: offline produces no framework call. */
    @Synchronized
    fun recordOfflineNoRequest(threadHash: Long): Event =
        Event(++sequence, 1, false, true, 0, 0, retainedHash, Result.NOT_APPLICABLE.code, 0, threadHash)

    /** I variant: initial Builder declaration is intentionally omitted. */
    @Synchronized
    fun recordInitialOmitted(selectedHash: Long, threadHash: Long): Event =
        Event(++sequence, 4, false, selectedHash == 0L, 0, selectedHash, retainedHash,
            Result.NOT_APPLICABLE.code, 0, threadHash)

    /** S/I variants: physical change is observed but no established framework write is made. */
    @Synchronized
    fun recordSuppressedEstablishedUpdate(selectedHash: Long, threadHash: Long): Event =
        Event(++sequence, 3, false, selectedHash == 0L, 0, selectedHash, retainedHash,
            Result.NOT_APPLICABLE.code, 0, threadHash)

    @Synchronized
    fun recordCallback(
        activeHash: Long,
        selectedHash: Long,
        activeIsVpn: Boolean,
        isDefault: Boolean,
        validated: Boolean,
        callbackCode: Int,
        declaredUnderlyingHash: Long = 0,
        threadHash: Long = 0,
    ): Event {
        val signature = listOf(activeHash, selectedHash, activeIsVpn, isDefault, validated, declaredUnderlyingHash)
        val duplicate = signature == lastCallbackSignature
        lastCallbackSignature = signature
        return Event(++sequence, 2, false, selectedHash == 0L, 0, 0, retainedHash,
            Result.NOT_APPLICABLE.code, 0, threadHash, callbackCode, activeHash, selectedHash,
            declaredUnderlyingHash, activeIsVpn, isDefault, validated, duplicate)
    }

    private companion object {
        fun Boolean.bit() = if (this) 1 else 0
    }
}
