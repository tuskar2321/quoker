package app.quoker.feature.settings

import app.cash.turbine.test
import app.quoker.core.common.AppInfo
import app.quoker.core.data.model.InactiveReason
import app.quoker.core.network.ErrorCodes
import app.quoker.feature.settings.ui.UiMessage
import kotlin.time.Duration.Companion.seconds
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

class SettingsViewModelTest {
    @get:Rule
    val main = MainDispatcherRule()

    private val auth = FakeAuthRepository()
    private val targets = FakeTargetsRepository()

    private fun viewModel() = SettingsViewModel(auth, targets, AppInfo("http://core", 42, "1.2.0"), recheckDelay = 4.seconds)

    @Test
    fun `loads account, targets and version`() = runTest(main.dispatcher) {
        auth.me = me(admin = true)
        targets.server = listOf(target(1), target(2, hidden = true), target(3, reason = InactiveReason.BOT_REMOVED))
        val vm = viewModel()
        assertFalse(vm.state.value.targetsLoaded)
        advanceUntilIdle()
        val s = vm.state.value
        assertTrue(s.isAdmin)
        assertEquals("1.2.0", s.versionName)
        assertTrue(s.targetsLoaded)
        assertEquals(listOf(true, false, true), s.targets.map { it.visible })
    }

    @Test
    fun `account error without data is shown in place, retry recovers`() = runTest(main.dispatcher) {
        auth.meError = quokerException(ErrorCodes.NETWORK)
        val vm = viewModel()
        advanceUntilIdle()
        assertEquals(ErrorCodes.NETWORK, vm.state.value.meError?.code)
        auth.meError = null
        vm.retry()
        advanceUntilIdle()
        assertNull(vm.state.value.meError)
        assertEquals(auth.me, vm.state.value.me)
    }

    @Test
    fun `resume reloads only after the first resume`() = runTest(main.dispatcher) {
        val vm = viewModel()
        advanceUntilIdle()
        vm.onResume()
        advanceUntilIdle()
        assertEquals(1, auth.meCalls)
        auth.me = me(linked = true)
        vm.onResume()
        advanceUntilIdle()
        assertEquals(2, auth.meCalls)
        assertEquals(2, targets.fetchCalls)
    }

    @Test
    fun `toggle is optimistic and reverts on failure`() = runTest(main.dispatcher) {
        targets.server = listOf(target(1))
        val vm = viewModel()
        advanceUntilIdle()
        val gate = CompletableDeferred<Unit>()
        targets.setHiddenGate = gate
        targets.setHiddenError = quokerException(ErrorCodes.INTERNAL)
        vm.setVisible(1, false)
        runCurrent()
        assertEquals(TargetRow(target(1), visible = false, saving = true), vm.state.value.targets.single())
        // A second tap while saving is ignored.
        vm.setVisible(1, true)
        gate.complete(Unit)
        advanceUntilIdle()
        assertEquals(listOf(1L to true), targets.setHiddenCalls)
        assertEquals(TargetRow(target(1), visible = true, saving = false), vm.state.value.targets.single())
        assertEquals(ErrorCodes.INTERNAL, (vm.state.value.message as UiMessage.Failure).error.code)
    }

    @Test
    fun `toggle success hides the target`() = runTest(main.dispatcher) {
        targets.server = listOf(target(1))
        val vm = viewModel()
        advanceUntilIdle()
        vm.setVisible(1, false)
        advanceUntilIdle()
        assertEquals(TargetRow(target(1, hidden = true), visible = false, saving = false), vm.state.value.targets.single())
        assertNull(vm.state.value.message)
    }

    @Test
    fun `refresh re-checks, fetches now and again after the delay`() = runTest(main.dispatcher) {
        val vm = viewModel()
        advanceUntilIdle()
        vm.refreshTargets()
        runCurrent()
        assertEquals(1, targets.refreshCalls)
        assertEquals(2, targets.fetchCalls)
        assertTrue(vm.state.value.refreshingTargets)
        assertEquals(UiMessage.Info(R.string.settings_targets_refresh_queued), vm.state.value.message)
        targets.server = listOf(target(9))
        advanceTimeBy(4.seconds + 1.seconds)
        assertEquals(3, targets.fetchCalls)
        assertFalse(vm.state.value.refreshingTargets)
        assertEquals(listOf(9L), vm.state.value.targets.map { it.target.id })
    }

    @Test
    fun `rate limited refresh shows the error`() = runTest(main.dispatcher) {
        val vm = viewModel()
        advanceUntilIdle()
        targets.refreshError = quokerException(ErrorCodes.RATE_LIMITED)
        vm.refreshTargets()
        advanceUntilIdle()
        assertEquals(ErrorCodes.RATE_LIMITED, (vm.state.value.message as UiMessage.Failure).error.code)
        assertFalse(vm.state.value.refreshingTargets)
    }

    @Test
    fun `unlink marks the account unlinked and refetches targets`() = runTest(main.dispatcher) {
        val vm = viewModel()
        advanceUntilIdle()
        vm.showDialog(SettingsDialog.Unlink)
        targets.server = listOf(target(1, reason = InactiveReason.USER_LEFT))
        vm.unlinkTelegram()
        assertNull(vm.state.value.dialog)
        assertTrue(vm.state.value.unlinking)
        advanceUntilIdle()
        assertEquals(1, auth.unlinkCalls)
        assertFalse(vm.state.value.telegram!!.linked)
        assertFalse(vm.state.value.unlinking)
        assertFalse(vm.state.value.targets.single().target.isActive)
    }

    @Test
    fun `unlink failure keeps the account and reports`() = runTest(main.dispatcher) {
        val vm = viewModel()
        advanceUntilIdle()
        auth.unlinkError = quokerException(ErrorCodes.NETWORK)
        vm.unlinkTelegram()
        advanceUntilIdle()
        assertTrue(vm.state.value.telegram!!.linked)
        assertEquals(ErrorCodes.NETWORK, (vm.state.value.message as UiMessage.Failure).error.code)
    }

    @Test
    fun `logout signs out and emits the event`() = runTest(main.dispatcher) {
        val vm = viewModel()
        advanceUntilIdle()
        vm.events.test {
            vm.showDialog(SettingsDialog.Logout)
            vm.logout()
            assertEquals(SettingsEvent.SignedOut, awaitItem())
        }
        assertEquals(1, auth.logoutCalls)
        assertNull(vm.state.value.dialog)
    }
}
