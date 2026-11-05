package app.quoker.feature.send

import app.cash.turbine.test
import app.quoker.core.data.model.InactiveReason
import app.quoker.core.data.model.TargetType
import app.quoker.core.data.model.TelegramAccount
import app.quoker.core.data.repository.SendTracker
import app.quoker.core.data.repository.TrackedSend
import app.quoker.core.network.ErrorCodes
import java.time.Instant
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

class SendViewModelTest {
    @get:Rule
    val main = MainDispatcherRule()

    private val targets = FakeTargetsRepository()
    private val sends = FakeSendsRepository()
    private val tracker = SendTracker()
    private val auth = FakeAuthRepository()
    private val videos = FakeVideoRepository()

    private fun viewModel() = SendViewModel(targets, sends, tracker, auth, videos)

    private val friends = target(42, "Друзья", TargetType.GROUP, lastUsedAt = Instant.parse("2026-11-02T12:40:00Z"))
    private val channel = target(43, "Мой канал", TargetType.CHANNEL)
    private val old = target(45, "Старый", TargetType.CHANNEL, active = false, reason = InactiveReason.BOT_NOT_ADMIN)
    private val hidden = target(46, "Скрытая", hidden = true)

    @Test
    fun `open fetches, orders targets and preselects the last used one`() = runTest(main.dispatcher) {
        targets.server = listOf(old, channel, friends, hidden)
        val vm = viewModel()
        vm.open("tiktok:1", "s1")
        assertTrue(vm.state.value.loading)
        advanceUntilIdle()

        val s = vm.state.value
        assertEquals(listOf(43L, 42L, 45L), s.targets.map { it.id })
        assertEquals(listOf(false, true, false), s.targets.map { it.isLastUsed })
        assertEquals(42L, s.selectedId)
        assertTrue(s.showSignToggle)
        assertEquals("@max", s.signName)
        assertEquals("https://www.tiktok.com/@a/video/1", s.captionFieldValue)
        assertNull(s.captionToSend)
        assertFalse(s.empty)
    }

    @Test
    fun `without a last used target the first active one is selected and inactive ones cannot be`() =
        runTest(main.dispatcher) {
            targets.server = listOf(old, channel)
            val vm = viewModel()
            vm.open("v", "s1")
            advanceUntilIdle()
            assertEquals(43L, vm.state.value.selectedId)
            assertFalse(vm.state.value.showSignToggle)

            vm.select(45)
            assertEquals(43L, vm.state.value.selectedId)
        }

    @Test
    fun `caption models untouched, cleared and edited values`() = runTest(main.dispatcher) {
        targets.server = listOf(channel)
        val vm = viewModel()
        vm.open("v", "s1")
        advanceUntilIdle()
        assertNull(vm.state.value.captionToSend)

        vm.onCaptionChange("")
        assertEquals("", vm.state.value.captionToSend)
        assertEquals("", vm.state.value.captionFieldValue)

        vm.onCaptionChange("привет")
        assertEquals("привет", vm.state.value.captionToSend)
    }

    @Test
    fun `caption is cut to 1024 UTF-16 units`() = runTest(main.dispatcher) {
        val vm = viewModel()
        vm.open("v", "s1")
        vm.onCaptionChange("a".repeat(2000))
        assertEquals(1024, vm.state.value.captionText.length)
        assertEquals(0, vm.state.value.captionLeft)
    }

    @Test
    fun `send uses one key per session, registers the send and emits Sent`() = runTest(main.dispatcher) {
        targets.server = listOf(friends)
        sends.errors += quokerException(ErrorCodes.RATE_LIMITED)
        val vm = viewModel()
        vm.open("tiktok:1", "s1")
        advanceUntilIdle()
        vm.onSignAsUserChange(false)

        vm.events.test {
            vm.send()
            advanceUntilIdle()
            assertEquals(ErrorCodes.RATE_LIMITED, vm.state.value.sendError?.code)
            assertFalse(vm.state.value.sending)

            vm.send()
            advanceUntilIdle()
            assertEquals(SendEvent.Sent, awaitItem())
        }
        assertEquals(2, sends.calls.size)
        assertEquals(setOf("key-1"), sends.calls.map { it.key }.toSet())
        assertEquals(CreateCall("tiktok:1", 42, null, false, "key-1"), sends.calls.last())
        assertEquals(TrackedSend(1002, "tiktok:1", 42, "Друзья"), tracker.observe("tiktok:1").first())
        assertNull(vm.state.value.sendError)
    }

    @Test
    fun `sign as user is sent only for groups`() = runTest(main.dispatcher) {
        targets.server = listOf(channel)
        val vm = viewModel()
        vm.open("v", "s1")
        advanceUntilIdle()
        assertTrue(vm.state.value.signAsUser)
        vm.send()
        advanceUntilIdle()
        assertFalse(sends.calls.single().signAsUser)
    }

    @Test
    fun `a new opening gets a new key and a fresh form, resuming keeps both`() = runTest(main.dispatcher) {
        targets.server = listOf(channel)
        val vm = viewModel()
        vm.open("v", "s1")
        advanceUntilIdle()
        vm.onCaptionChange("x")
        val key = vm.currentIdempotencyKey

        vm.close()
        vm.open("v", "s1")
        advanceUntilIdle()
        assertEquals(key, vm.currentIdempotencyKey)
        assertEquals("x", vm.state.value.captionText)

        vm.open("v", "s2")
        advanceUntilIdle()
        assertTrue(key != vm.currentIdempotencyKey)
        assertFalse(vm.state.value.captionTouched)
        assertEquals("s2", vm.state.value.sessionId)
    }

    @Test
    fun `telegram_not_linked asks to link Telegram`() = runTest(main.dispatcher) {
        targets.server = listOf(channel)
        sends.errors += quokerException(ErrorCodes.TELEGRAM_NOT_LINKED)
        val vm = viewModel()
        vm.open("v", "s1")
        advanceUntilIdle()
        vm.events.test {
            vm.send()
            advanceUntilIdle()
            assertEquals(SendEvent.LinkTelegram, awaitItem())
        }
        assertNull(vm.state.value.sendError)
    }

    @Test
    fun `target_inactive shows the error and refetches targets`() = runTest(main.dispatcher) {
        targets.server = listOf(channel, friends)
        val vm = viewModel()
        vm.open("v", "s1")
        advanceUntilIdle()
        sends.errors += quokerException(ErrorCodes.TARGET_INACTIVE)
        targets.server = listOf(channel, friends.copy(status = app.quoker.core.data.model.TargetStatus.INACTIVE))
        vm.send()
        advanceUntilIdle()

        val s = vm.state.value
        assertEquals(ErrorCodes.TARGET_INACTIVE, s.sendError?.code)
        assertEquals(43L, s.selectedId)
        assertEquals(2, targets.fetchCalls)
    }

    @Test
    fun `empty list and refresh fetches now and again later`() = runTest(main.dispatcher) {
        val vm = viewModel()
        vm.open("v", "s1")
        advanceUntilIdle()
        assertTrue(vm.state.value.empty)
        assertNull(vm.state.value.selectedId)

        targets.refreshError = quokerException(ErrorCodes.RATE_LIMITED)
        targets.server = listOf(channel)
        vm.refreshList()
        advanceUntilIdle()

        assertEquals(1, targets.refreshCalls)
        assertEquals(3, targets.fetchCalls)
        assertFalse(vm.state.value.empty)
        assertFalse(vm.state.value.refreshing)
        assertEquals(43L, vm.state.value.selectedId)
    }

    @Test
    fun `first fetch failure without cache is a load error with retry`() = runTest(main.dispatcher) {
        targets.fetchError = quokerException(ErrorCodes.NETWORK)
        val vm = viewModel()
        vm.open("v", "s1")
        advanceUntilIdle()
        assertTrue(vm.state.value.loadFailed)

        targets.fetchError = null
        targets.server = listOf(channel)
        vm.retryFetch()
        advanceUntilIdle()
        assertFalse(vm.state.value.loadFailed)
        assertEquals(43L, vm.state.value.selectedId)
    }

    @Test
    fun `sign name falls back to the first name and video errors keep an empty default`() =
        runTest(main.dispatcher) {
            auth.telegram = TelegramAccount(linked = true, username = null, firstName = "Макс")
            videos.getError = quokerException(ErrorCodes.NOT_FOUND)
            val vm = viewModel()
            vm.open("v", "s1")
            advanceUntilIdle()
            assertEquals("Макс", vm.state.value.signName)
            assertEquals("", vm.state.value.captionFieldValue)
            assertNull(vm.state.value.captionToSend)
        }

    private data class RetryCase(
        val name: String,
        val server: List<app.quoker.core.data.model.Target>,
        val existing: Map<Long, Long>,
        val retrySendId: Long?,
        val expected: Long?,
    )

    @Test
    fun `retry preselects the failed send's target when it is still active`() = runTest(main.dispatcher) {
        val cases = listOf(
            RetryCase("retry to a not-last-used target", listOf(channel, friends), mapOf(7L to 43L), 7L, 43L),
            RetryCase("no retry keeps the last used", listOf(channel, friends), mapOf(7L to 43L), null, 42L),
            RetryCase("unknown send falls back", listOf(channel, friends), emptyMap(), 7L, 42L),
            RetryCase("inactive target falls back", listOf(channel, friends, old), mapOf(7L to 45L), 7L, 42L),
            RetryCase("hidden target falls back", listOf(channel, friends, hidden), mapOf(7L to 46L), 7L, 42L),
        )
        for (case in cases) {
            targets.server = case.server
            sends.existing.clear()
            sends.existing.putAll(case.existing)
            val vm = viewModel()
            vm.open("v", "s-${case.name}", case.retrySendId)
            advanceUntilIdle()
            assertEquals(case.name, case.expected, vm.state.value.selectedId)
        }
    }

    @Test
    fun `a target picked by the user wins over a late retry target`() = runTest(main.dispatcher) {
        targets.cache.value = listOf(channel, friends)
        targets.server = listOf(channel, friends)
        sends.existing[7L] = 43L
        sends.getGate = kotlinx.coroutines.CompletableDeferred()
        val vm = viewModel()
        vm.open("v", "s1", retrySendId = 7L)
        advanceUntilIdle()
        assertEquals(42L, vm.state.value.selectedId)

        vm.select(42)
        sends.getGate!!.complete(Unit)
        advanceUntilIdle()
        assertEquals(42L, vm.state.value.selectedId)
    }

    @Test
    fun `a new session without retry drops the previous retry target`() = runTest(main.dispatcher) {
        targets.server = listOf(channel, friends)
        sends.existing[7L] = 43L
        val vm = viewModel()
        vm.open("v", "s1", retrySendId = 7L)
        advanceUntilIdle()
        assertEquals(43L, vm.state.value.selectedId)

        vm.close()
        vm.open("v", "s2")
        advanceUntilIdle()
        assertEquals(42L, vm.state.value.selectedId)
    }
}
