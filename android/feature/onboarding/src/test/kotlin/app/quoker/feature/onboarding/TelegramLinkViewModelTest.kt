package app.quoker.feature.onboarding

import app.cash.turbine.test
import app.quoker.core.common.Clock
import app.quoker.core.data.model.TelegramLink
import app.quoker.core.data.model.TelegramLinkState
import app.quoker.core.data.model.TelegramLinkStatus
import app.quoker.feature.onboarding.telegram.TelegramLinkEvent
import app.quoker.feature.onboarding.telegram.TelegramLinkPhase
import app.quoker.feature.onboarding.telegram.TelegramLinkViewModel
import java.time.Instant
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

class TelegramLinkViewModelTest {
    @get:Rule
    val main = MainDispatcherRule()

    private var now = Instant.parse("2026-11-04T12:00:00Z")
    private val auth = FakeAuthRepository()
    private val vm = TelegramLinkViewModel(auth, Clock { now })

    private fun status(state: TelegramLinkState) = TelegramLinkStatus(state, null, null)

    @Test
    fun openCreatesLinkOpensItAndPollsWhileVisible() = runTest {
        vm.onVisible()
        assertEquals(0, auth.pollStatuses.subscriptionCount.value)
        vm.events.test {
            vm.onOpenBot()
            assertEquals(TelegramLinkEvent.OpenLink("https://t.me/bot?start=link_1"), awaitItem())
            assertEquals(TelegramLinkPhase.Waiting, vm.state.value.phase)
            assertEquals(1, auth.pollStatuses.subscriptionCount.value)

            auth.pollStatuses.emit(status(TelegramLinkState.PENDING))
            auth.pollStatuses.emit(status(TelegramLinkState.LINKED))
            assertEquals(TelegramLinkEvent.Linked, awaitItem())
        }
    }

    @Test
    fun pollingStopsWhenHiddenAndResumesWhenVisible() = runTest {
        vm.onVisible()
        vm.onOpenBot()
        assertEquals(1, auth.pollStatuses.subscriptionCount.value)
        vm.onHidden()
        assertEquals(0, auth.pollStatuses.subscriptionCount.value)
        vm.onVisible()
        assertEquals(1, auth.pollStatuses.subscriptionCount.value)
    }

    @Test
    fun noPollingWhileHidden() = runTest {
        vm.onOpenBot()
        assertEquals(TelegramLinkPhase.Waiting, vm.state.value.phase)
        assertEquals(0, auth.pollStatuses.subscriptionCount.value)
    }

    @Test
    fun expiredThenRenewCreatesNewLink() = runTest {
        vm.onVisible()
        vm.onOpenBot()
        auth.pollStatuses.emit(status(TelegramLinkState.EXPIRED))
        assertEquals(TelegramLinkPhase.Expired, vm.state.value.phase)
        vm.events.test {
            skipItems(1) // the first OpenLink
            vm.onRenew()
            assertEquals(TelegramLinkEvent.OpenLink("https://t.me/bot?start=link_2"), awaitItem())
        }
        assertEquals(TelegramLinkPhase.Waiting, vm.state.value.phase)
    }

    @Test
    fun reopensValidLinkButReplacesExpiredOne() = runTest {
        auth.linkResult = { auth.linksCreated++; TelegramLink("https://t.me/bot?start=link_${auth.linksCreated}", now.plusSeconds(600)) }
        vm.onOpenBot()
        vm.onOpenBot()
        assertEquals(1, auth.linksCreated)
        now = now.plusSeconds(601)
        vm.onOpenBot()
        assertEquals(2, auth.linksCreated)
    }

    @Test
    fun createErrorAndRetry() = runTest {
        auth.linkResult = { throw quokerException("rate_limited") }
        vm.onOpenBot()
        assertEquals("rate_limited", vm.state.value.error?.code)
        assertFalse(vm.state.value.creatingLink)
        assertEquals(TelegramLinkPhase.Idle, vm.state.value.phase)

        auth.linkResult = { TelegramLink("https://t.me/bot?start=ok", null) }
        vm.onRetry()
        assertEquals(TelegramLinkPhase.Waiting, vm.state.value.phase)
        assertEquals(null, vm.state.value.error)
    }

    @Test
    fun pollFailureShowsErrorAndRetryResumes() = runTest {
        vm.onVisible()
        auth.pollFailure = quokerException("network", retryable = true)
        vm.onOpenBot()
        assertEquals("network", vm.state.value.error?.code)
        auth.pollFailure = null
        vm.onRetry()
        assertEquals(1, auth.pollStatuses.subscriptionCount.value)
    }

    @Test
    fun openFailedFlag() {
        vm.onOpenFailed()
        assertTrue(vm.state.value.openFailed)
    }
}
