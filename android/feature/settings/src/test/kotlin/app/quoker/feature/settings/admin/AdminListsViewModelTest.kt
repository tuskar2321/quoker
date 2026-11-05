package app.quoker.feature.settings.admin

import app.quoker.core.common.Clock
import app.quoker.core.data.model.Invite
import app.quoker.core.data.model.Role
import app.quoker.core.data.model.UserStatus
import app.quoker.core.data.model.WorkerStatus
import app.quoker.core.network.ErrorCodes
import app.quoker.feature.settings.FakeAdminRepository
import app.quoker.feature.settings.FakeAuthRepository
import app.quoker.feature.settings.MainDispatcherRule
import app.quoker.feature.settings.me
import app.quoker.feature.settings.quokerException
import app.quoker.feature.settings.user
import app.quoker.feature.settings.worker
import app.quoker.feature.settings.ui.UiMessage
import java.time.Instant
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

class InvitesViewModelTest {
    @get:Rule
    val main = MainDispatcherRule()

    private val now = Instant.parse("2026-11-02T12:00:00Z")
    private val admin = FakeAdminRepository()
    private fun viewModel() = InvitesViewModel(admin, Clock { now })

    @Test
    fun `creates with the chosen role and validity and shows the code once`() = runTest(main.dispatcher) {
        val vm = viewModel()
        advanceUntilIdle()
        assertEquals(emptyList<Invite>(), vm.state.value.invites)
        vm.selectRole(Role.ADMIN)
        vm.selectDays(30)
        vm.create()
        assertTrue(vm.state.value.creating)
        advanceUntilIdle()
        assertEquals(listOf(Role.ADMIN to 30), admin.created)
        assertEquals(NewInvite("K7Q2-M4XP", Role.ADMIN, admin.nextExpiry), vm.state.value.created)
        assertEquals(listOf("K7Q2"), vm.state.value.invites!!.map { it.codePrefix })
        assertFalse(vm.state.value.creating)
    }

    @Test
    fun `defaults to a user invite for 7 days`() = runTest(main.dispatcher) {
        val vm = viewModel()
        vm.create()
        advanceUntilIdle()
        assertEquals(listOf(Role.USER to 7), admin.created)
    }

    @Test
    fun `create failure reports and shows no code`() = runTest(main.dispatcher) {
        admin.actionError = quokerException(ErrorCodes.INVALID_REQUEST)
        val vm = viewModel()
        vm.create()
        advanceUntilIdle()
        assertNull(vm.state.value.created)
        assertEquals(ErrorCodes.INVALID_REQUEST, (vm.state.value.message as UiMessage.Failure).error.code)
    }

    @Test
    fun `revoke asks first, then removes the invite`() = runTest(main.dispatcher) {
        val invite = Invite(1, Role.USER, "B8RW", null, now.plusSeconds(3600), null, null)
        admin.invites = mutableListOf(invite)
        val vm = viewModel()
        advanceUntilIdle()
        vm.askRevoke(invite)
        assertEquals(invite, vm.state.value.confirmRevoke)
        vm.dismissRevoke()
        vm.revoke()
        advanceUntilIdle()
        assertTrue(admin.revokedInvites.isEmpty())
        vm.askRevoke(invite)
        vm.revoke()
        advanceUntilIdle()
        assertEquals(listOf(1L), admin.revokedInvites)
        assertEquals(emptyList<Invite>(), vm.state.value.invites)
        assertNull(vm.state.value.revokingId)
    }

    @Test
    fun `used invites get the user name`() = runTest(main.dispatcher) {
        admin.users = mutableListOf(user(7, username = "friend"), user(8, username = null))
        val vm = viewModel()
        advanceUntilIdle()
        assertEquals(mapOf(7L to "@friend"), vm.state.value.userNames)
    }
}

class UsersViewModelTest {
    @get:Rule
    val main = MainDispatcherRule()

    private val admin = FakeAdminRepository()
    private val auth = FakeAuthRepository()
    private fun viewModel() = UsersViewModel(admin, auth, Clock { Instant.EPOCH })

    @Test
    fun `own account first and counters`() = runTest(main.dispatcher) {
        auth.me = me(id = 2, admin = true)
        admin.users = mutableListOf(user(1), user(2, role = Role.ADMIN), user(3, UserStatus.BLOCKED))
        val vm = viewModel()
        advanceUntilIdle()
        val s = vm.state.value
        assertEquals(listOf(2L, 1L, 3L), s.users!!.map { it.id })
        assertEquals(Triple(3, 2, 1), Triple(s.total, s.active, s.blocked))
    }

    @Test
    fun `block and unblock after confirmation`() = runTest(main.dispatcher) {
        auth.me = me(id = 1, admin = true)
        admin.users = mutableListOf(user(1), user(2), user(3, UserStatus.BLOCKED))
        val vm = viewModel()
        advanceUntilIdle()
        val users = vm.state.value.users!!
        vm.ask(users.first { it.id == 2L })
        assertEquals(UserAction(users[1], block = true), vm.state.value.confirm)
        vm.confirm()
        advanceUntilIdle()
        vm.ask(users.first { it.id == 3L })
        assertFalse(vm.state.value.confirm!!.block)
        vm.confirm()
        advanceUntilIdle()
        assertEquals(listOf(2L), admin.blocked)
        assertEquals(listOf(3L), admin.unblocked)
        assertEquals(
            listOf(UserStatus.ACTIVE, UserStatus.BLOCKED, UserStatus.ACTIVE),
            vm.state.value.users!!.map { it.status },
        )
    }

    @Test
    fun `own account has no actions`() = runTest(main.dispatcher) {
        auth.me = me(id = 1, admin = true)
        admin.users = mutableListOf(user(1))
        val vm = viewModel()
        advanceUntilIdle()
        vm.ask(vm.state.value.users!!.single())
        assertNull(vm.state.value.confirm)
    }

    @Test
    fun `block failure keeps the status`() = runTest(main.dispatcher) {
        admin.users = mutableListOf(user(5))
        val vm = viewModel()
        advanceUntilIdle()
        admin.actionError = quokerException(ErrorCodes.FORBIDDEN)
        vm.ask(vm.state.value.users!!.single())
        vm.confirm()
        advanceUntilIdle()
        assertEquals(UserStatus.ACTIVE, vm.state.value.users!!.single().status)
        assertEquals(ErrorCodes.FORBIDDEN, (vm.state.value.message as UiMessage.Failure).error.code)
    }
}

class WorkersViewModelTest {
    @get:Rule
    val main = MainDispatcherRule()

    private val admin = FakeAdminRepository()
    private fun viewModel() = WorkersViewModel(admin, Clock { Instant.EPOCH })

    @Test
    fun `sorted allowed online first, revoked last`() = runTest(main.dispatcher) {
        admin.workers = mutableListOf(
            worker("c", status = WorkerStatus.REVOKED, connected = false),
            worker("b", connected = false),
            worker("a"),
        )
        val vm = viewModel()
        advanceUntilIdle()
        assertEquals(listOf("a", "b", "c"), vm.state.value.workers!!.map { it.id })
    }

    @Test
    fun `fingerprint validation`() {
        data class Case(val input: String, val valid: Boolean)
        listOf(
            Case("ab".repeat(32), true),
            Case("AB:".repeat(31) + "AB", true),
            Case("ab ".repeat(32), true),
            Case("ab".repeat(31), false),
            Case("zz".repeat(32), false),
        ).forEach { assertEquals(it.input, it.valid, AllowWorkerForm(id = "w", fingerprint = it.input).fingerprintValid) }
        assertFalse(AllowWorkerForm(id = " ", fingerprint = "ab".repeat(32)).canSubmit)
        assertFalse(AllowWorkerForm(fingerprint = "").showFingerprintError)
    }

    @Test
    fun `allow sends the normalized fingerprint and reloads`() = runTest(main.dispatcher) {
        val vm = viewModel()
        advanceUntilIdle()
        vm.openAllow()
        vm.onAllowIdChange(" worker-2 ")
        vm.onAllowFingerprintChange("AB:".repeat(31) + "AB")
        vm.allow()
        advanceUntilIdle()
        assertEquals(listOf("worker-2" to "ab".repeat(32)), admin.allowed)
        assertNull(vm.state.value.allowForm)
        assertEquals(listOf("worker-2"), vm.state.value.workers!!.map { it.id })
    }

    @Test
    fun `allow failure stays in the dialog with the error`() = runTest(main.dispatcher) {
        admin.actionError = quokerException(ErrorCodes.INVALID_REQUEST)
        val vm = viewModel()
        vm.openAllow()
        vm.onAllowIdChange("w")
        vm.onAllowFingerprintChange("ab".repeat(32))
        vm.allow()
        advanceUntilIdle()
        val form = vm.state.value.allowForm!!
        assertFalse(form.submitting)
        assertEquals(ErrorCodes.INVALID_REQUEST, form.error?.code)
    }

    @Test
    fun `revoke after confirmation`() = runTest(main.dispatcher) {
        admin.workers = mutableListOf(worker("w1"))
        val vm = viewModel()
        advanceUntilIdle()
        vm.askRevoke(vm.state.value.workers!!.single())
        vm.revoke()
        advanceUntilIdle()
        assertEquals(listOf("w1"), admin.revokedWorkers)
        assertEquals(WorkerStatus.REVOKED, vm.state.value.workers!!.single().status)
        assertNull(vm.state.value.revokingId)
    }
}

class CertExpiryTest {
    @Test
    fun `expiry follows the 30-day warning of the core`() {
        val now = Instant.parse("2026-11-05T12:00:00Z")
        val day = 86_400L
        val cases = listOf(
            now.plusSeconds(day * 365) to CertExpiry.Valid,
            now.plusSeconds(day * 30) to CertExpiry.Valid,
            now.plusSeconds(day * 30 - 1) to CertExpiry.ExpiresSoon,
            now.plusSeconds(1) to CertExpiry.ExpiresSoon,
            now to CertExpiry.Expired,
            now.minusSeconds(day) to CertExpiry.Expired,
        )
        for ((notAfter, expected) in cases) assertEquals(notAfter.toString(), expected, certExpiry(notAfter, now))
    }
}
