package app.quoker.feature.settings.admin

import app.quoker.core.common.Clock
import app.quoker.core.data.model.Invite
import app.quoker.core.data.model.Role
import app.quoker.core.data.model.UserStatus
import app.quoker.core.network.ErrorCodes
import app.quoker.feature.settings.FakeAdminRepository
import app.quoker.feature.settings.FakeCookiesFileReader
import app.quoker.feature.settings.FakeCookiesUploadStore
import app.quoker.feature.settings.MainDispatcherRule
import app.quoker.feature.settings.R
import app.quoker.feature.settings.adminStatus
import app.quoker.feature.settings.data.CookiesFile
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

class AdminStatusViewModelTest {
    @get:Rule
    val main = MainDispatcherRule()

    private val now = Instant.parse("2026-11-05T12:00:00Z")
    private val admin = FakeAdminRepository()
    private val reader = FakeCookiesFileReader()

    private fun viewModel(store: FakeCookiesUploadStore = FakeCookiesUploadStore()) =
        AdminStatusViewModel(admin, store, reader, Clock { now })

    private fun daysAgo(days: Long) = now.minusSeconds(days * 24 * 3600)

    @Test
    fun `alerts follow the status`() {
        data class Case(val name: String, val state: AdminStatusUiState, val expected: List<AdminAlert>)
        val cases = listOf(
            Case("healthy", AdminStatusUiState(status = adminStatus()), emptyList()),
            Case(
                "no workers online",
                AdminStatusUiState(status = adminStatus(workers = listOf(worker("w", connected = false)))),
                listOf(AdminAlert.NoWorkers),
            ),
            Case("instagram errors", AdminStatusUiState(status = adminStatus(instagramErrors = 3)), listOf(AdminAlert.InstagramErrors(3))),
            Case("disk at 80% is fine", AdminStatusUiState(status = adminStatus(usedBytes = 80)), emptyList()),
            Case("disk over 80%", AdminStatusUiState(status = adminStatus(usedBytes = 86)), listOf(AdminAlert.DiskAlmostFull(86, 10))),
            Case("not loaded", AdminStatusUiState(), emptyList()),
        )
        cases.forEach { assertEquals(it.name, it.expected, it.state.alerts) }
    }

    @Test
    fun `cookies age and staleness`() {
        data class Case(val uploadedAt: Instant?, val days: Long?, val stale: Boolean)
        listOf(
            Case(null, null, false),
            Case(now.minusSeconds(3600), 0, false),
            Case(daysAgo(14), 14, false),
            Case(daysAgo(21), 21, true),
        ).forEach {
            val s = AdminStatusUiState(cookiesUploadedAt = it.uploadedAt, now = now)
            assertEquals(it.days, s.cookiesAgeDays)
            assertEquals(it.stale, s.cookiesStale)
        }
    }

    @Test
    fun `loads status and counters`() = runTest(main.dispatcher) {
        admin.invites = mutableListOf(
            Invite(1, Role.USER, "AAAA", null, now.plusSeconds(60), null, null),
            Invite(2, Role.USER, "BBBB", null, now.minusSeconds(60), null, null),
            Invite(3, Role.USER, "CCCC", null, now.plusSeconds(60), 5, now),
        )
        admin.users = mutableListOf(user(1), user(2, UserStatus.BLOCKED), user(3))
        val vm = viewModel(FakeCookiesUploadStore(daysAgo(3)))
        advanceUntilIdle()
        val s = vm.state.value
        assertEquals(admin.status, s.status)
        assertEquals(1, s.activeInvites)
        assertEquals(UsersSummary(3, 1), s.users)
        assertEquals(3L, s.cookiesAgeDays)
        assertEquals("worker-1", s.poller?.id)
    }

    @Test
    fun `status error is a load error`() = runTest(main.dispatcher) {
        admin.statusError = quokerException(ErrorCodes.FORBIDDEN)
        val vm = viewModel()
        advanceUntilIdle()
        assertEquals(ErrorCodes.FORBIDDEN, vm.state.value.error?.code)
        admin.statusError = null
        vm.retry()
        advanceUntilIdle()
        assertNull(vm.state.value.error)
    }

    @Test
    fun `cookies upload stores the time`() = runTest(main.dispatcher) {
        val store = FakeCookiesUploadStore(daysAgo(30))
        val vm = viewModel(store)
        advanceUntilIdle()
        assertTrue(vm.state.value.cookiesStale)
        vm.onCookiesFilePicked("content://cookies.txt")
        assertTrue(vm.state.value.uploadingCookies)
        advanceUntilIdle()
        assertEquals(listOf("content://cookies.txt"), reader.reads)
        assertEquals(listOf("# Netscape HTTP Cookie File"), admin.uploadedCookies)
        assertEquals(0L, vm.state.value.cookiesAgeDays)
        assertFalse(vm.state.value.cookiesStale)
        assertFalse(vm.state.value.uploadingCookies)
        assertEquals(UiMessage.Info(R.string.admin_cookies_uploaded), vm.state.value.message)
    }

    @Test
    fun `bad files are not uploaded`() = runTest(main.dispatcher) {
        listOf(
            CookiesFile.TooLarge to R.string.admin_cookies_too_large,
            CookiesFile.Empty to R.string.admin_cookies_empty,
            CookiesFile.Unreadable to R.string.admin_cookies_unreadable,
        ).forEach { (file, text) ->
            reader.result = file
            val vm = viewModel()
            vm.onCookiesFilePicked("content://x")
            advanceUntilIdle()
            assertEquals(UiMessage.ErrorText(text), vm.state.value.message)
        }
        assertTrue(admin.uploadedCookies.isEmpty())
    }

    @Test
    fun `upload failure keeps the old date`() = runTest(main.dispatcher) {
        admin.actionError = quokerException(ErrorCodes.INVALID_REQUEST)
        val vm = viewModel(FakeCookiesUploadStore(daysAgo(20)))
        vm.onCookiesFilePicked("content://x")
        advanceUntilIdle()
        assertEquals(20L, vm.state.value.cookiesAgeDays)
        assertEquals(ErrorCodes.INVALID_REQUEST, (vm.state.value.message as UiMessage.Failure).error.code)
    }
}
