package io.nekohasekai.sagernet.ui

import org.junit.Assert.*
import org.junit.Test

class DashboardHealthTest {
    @Test fun networkRecoveryInvalidatesOldProbeAndRequiresNewMeasurement() {
        val health = DashboardHealth()
        health.serviceChanged(1, true)
        val old = health.beginTest(1)!!
        assertTrue(health.networkChanged("cellular-1"))
        assertFalse(health.complete(old, DashboardConnectionTestResult.Success(1)))
        assertEquals(DashboardHealth.Tone.GRAY, health.tone)
        assertFalse(health.networkChanged("cellular-1"))
        val fresh = health.beginTest(1)!!
        assertTrue(health.complete(fresh, DashboardConnectionTestResult.Success(20)))
        assertEquals(DashboardHealth.Tone.GREEN, health.tone)
    }
    @Test fun onlyMeasuredSuccessIsGreen() {
        val health = DashboardHealth()
        health.serviceChanged(1, connected = true)
        assertEquals(DashboardHealth.Tone.GRAY, health.tone)
        val ticket = health.beginTest(1)!!
        assertEquals(DashboardHealth.Tone.GRAY, health.tone)
        health.complete(ticket, DashboardConnectionTestResult.Success(42))
        assertEquals(DashboardHealth.Tone.GREEN, health.tone)
    }

    @Test fun failureSurvivesAutomaticStopUntilExplicitAction() {
        val health = DashboardHealth()
        health.serviceChanged(1, true)
        health.complete(health.beginTest(1)!!, DashboardConnectionTestResult.Timeout)
        health.serviceChanged(1, false)
        assertEquals(DashboardHealth.Tone.RED, health.tone)
        health.reset()
        assertEquals(DashboardHealth.Tone.GRAY, health.tone)
        health.serviceChanged(1, false, DashboardConnectionTestResult.Failure("service error"))
        health.serviceChanged(1, false)
        assertEquals(DashboardHealth.Tone.RED, health.tone)
    }

    @Test fun oldSessionAndProfileResultsCannotOverwriteCurrentHealth() {
        val health = DashboardHealth()
        health.serviceChanged(1, true)
        val old = health.beginTest(1)!!
        assertNull(health.beginTest(1))
        health.serviceChanged(1, false)
        health.serviceChanged(1, true)
        assertFalse(health.complete(old, DashboardConnectionTestResult.Success(1)))
        val other = health.beginTest(1)!!
        health.serviceChanged(2, true)
        assertFalse(health.complete(other, DashboardConnectionTestResult.Success(1)))
        assertEquals(DashboardHealth.Tone.GRAY, health.tone)
    }

    @Test fun timeoutRejectsLateSuccessAndManualRetryCanRecover() {
        val health = DashboardHealth()
        health.serviceChanged(1, true)
        val ticket = health.beginTest(1)!!
        assertTrue(health.complete(ticket, DashboardConnectionTestResult.Timeout))
        assertFalse(health.complete(ticket, DashboardConnectionTestResult.Success(80)))
        assertEquals(DashboardHealth.Tone.RED, health.tone)
        val retry = health.beginTest(1)!!
        assertEquals(DashboardHealth.Tone.GRAY, health.tone)
        assertTrue(health.complete(retry, DashboardConnectionTestResult.Success(40)))
        assertEquals(DashboardHealth.Tone.GREEN, health.tone)
    }

    @Test fun pageRefreshKeepsCurrentTestAndSuccessfulDisconnectTurnsGray() {
        val health = DashboardHealth()
        health.serviceChanged(1, true)
        val ticket = health.beginTest(1)!!
        health.serviceChanged(1, true)
        assertTrue(health.complete(ticket, DashboardConnectionTestResult.Success(10)))
        health.serviceChanged(1, true)
        assertEquals(DashboardHealth.Tone.GREEN, health.tone)
        health.serviceChanged(1, false)
        assertEquals(DashboardHealth.Tone.GRAY, health.tone)
    }

    @Test fun changingIdleProfileClearsPreviousFailure() {
        val health = DashboardHealth()
        health.serviceChanged(1, false, DashboardConnectionTestResult.Timeout)
        health.serviceChanged(2, false)
        assertEquals(DashboardHealth.Tone.GRAY, health.tone)
        assertNull(health.result)
    }

    @Test fun subscriptionUpdatesOnlyAllowIdleSubscriptionGroups() {
        assertTrue(canUpdateDashboardSubscription(true, true, false))
        assertFalse(canUpdateDashboardSubscription(false, true, false))
        assertFalse(canUpdateDashboardSubscription(true, false, false))
        assertFalse(canUpdateDashboardSubscription(true, true, true))
    }
}
