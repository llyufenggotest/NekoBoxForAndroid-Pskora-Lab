package io.nekohasekai.sagernet.ui

import java.net.SocketTimeoutException
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class DashboardConnectionTestResultTest {
    @Test fun successCarriesMeasuredLatency() {
        assertEquals(63, DashboardConnectionTestResult.Success(63).elapsedMs)
    }

    @Test fun timeoutIsExplicit() {
        assertTrue(dashboardConnectionTestFailure(SocketTimeoutException("connect timed out")) is DashboardConnectionTestResult.Timeout)
        assertTrue(dashboardConnectionTestFailure(IllegalStateException("context deadline exceeded")) is DashboardConnectionTestResult.Timeout)
    }

    @Test fun errorKeepsReadableReasonButRedactsAddressAndCredentials() {
        val result = dashboardConnectionTestFailure(
            IllegalStateException("dial vless://user:secret@203.0.113.7:443 failed token=abcd")
        ) as DashboardConnectionTestResult.Failure
        assertTrue(result.reason.contains("failed"))
        assertTrue(result.reason.contains("地址已隐藏"))
        assertFalse(result.reason.contains("203.0.113.7"))
        assertFalse(result.reason.contains("secret"))
        assertFalse(result.reason.contains("abcd"))
    }

    @Test fun staleTargetGuardRejectsChangedGenerationOrProfile() {
        fun current(startGeneration: Long, nowGeneration: Long, target: Long, nowTarget: Long) =
            startGeneration == nowGeneration && target == nowTarget
        assertTrue(current(4, 4, 19, 19))
        assertFalse(current(4, 5, 19, 19))
        assertFalse(current(4, 4, 19, 20))
    }
}
