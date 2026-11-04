package app.quoker.feature.history

import app.quoker.core.common.Clock
import app.quoker.core.data.model.HistoryActionType.DOWNLOADED
import app.quoker.core.data.model.HistoryPage
import app.quoker.core.network.ErrorCodes
import java.time.Instant
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

class HistoryViewModelTest {
    @get:Rule
    val main = MainDispatcherRule()

    private val now = Instant.parse("2026-11-04T15:00:00Z")
    private val repo = FakeHistoryRepository()
    private fun viewModel() = HistoryViewModel(repo, Clock { now })

    private fun ids(vm: HistoryViewModel) = vm.state.value.sections.flatMap { s -> s.cards.map { it.id } }

    private fun at(id: Long) = item(id, action(DOWNLOADED, now.minusSeconds(id * 60)))

    @Test
    fun `cached page shows at once, then the refreshed one`() = runTest(main.dispatcher) {
        repo.cache.value = HistoryPage(listOf(at(1)), null)
        repo.firstPage = HistoryPage(listOf(at(2), at(1)), "c1")
        val vm = viewModel()
        main.dispatcher.scheduler.runCurrent()
        // The cache emission is processed before the refresh answers.
        advanceUntilIdle()
        assertEquals(listOf(2L, 1L), ids(vm))
        assertTrue(vm.state.value.loaded)
        assertFalse(vm.state.value.endReached)
        assertFalse(vm.state.value.refreshing)
        assertEquals(1, repo.refreshCalls)
    }

    @Test
    fun `empty cache shows a skeleton, then the empty state`() = runTest(main.dispatcher) {
        val vm = viewModel()
        assertTrue(vm.state.value.showSkeleton)
        advanceUntilIdle()
        assertTrue(vm.state.value.showEmpty)
    }

    @Test
    fun `network error without cache is an error state, with cache an offline banner`() = runTest(main.dispatcher) {
        repo.refreshError = quokerException(ErrorCodes.NETWORK)
        val vm = viewModel()
        advanceUntilIdle()
        assertTrue(vm.state.value.showError)
        assertFalse(vm.state.value.offline)

        repo.cache.value = HistoryPage(listOf(at(1)), null)
        advanceUntilIdle()
        assertTrue(vm.state.value.offline)
        assertNull(vm.state.value.bannerError)

        repo.refreshError = null
        repo.firstPage = HistoryPage(listOf(at(1)), null)
        vm.refresh()
        advanceUntilIdle()
        assertFalse(vm.state.value.offline)
        assertNull(vm.state.value.error)
    }

    @Test
    fun `other refresh errors over content show an error banner`() = runTest(main.dispatcher) {
        repo.cache.value = HistoryPage(listOf(at(1)), null)
        repo.refreshError = quokerException(ErrorCodes.INTERNAL)
        val vm = viewModel()
        advanceUntilIdle()
        assertEquals(ErrorCodes.INTERNAL, vm.state.value.bannerError?.code)
        assertFalse(vm.state.value.offline)
    }

    @Test
    fun `load more follows cursors until the end`() = runTest(main.dispatcher) {
        repo.firstPage = HistoryPage(listOf(at(1)), "c1")
        repo.pages["c1"] = HistoryPage(listOf(at(2), at(1)), "c2")
        repo.pages["c2"] = HistoryPage(listOf(at(3)), null)
        val vm = viewModel()
        advanceUntilIdle()

        vm.loadMore()
        vm.loadMore() // ignored while loading
        advanceUntilIdle()
        assertEquals(listOf(1L, 2L), ids(vm))

        vm.loadMore()
        advanceUntilIdle()
        assertEquals(listOf(1L, 2L, 3L), ids(vm))
        assertTrue(vm.state.value.endReached)

        vm.loadMore()
        advanceUntilIdle()
        assertEquals(listOf("c1", "c2"), repo.pageCalls)
    }

    @Test
    fun `load more error needs an explicit retry`() = runTest(main.dispatcher) {
        repo.firstPage = HistoryPage(listOf(at(1)), "c1")
        repo.pageError = quokerException(ErrorCodes.NETWORK)
        val vm = viewModel()
        advanceUntilIdle()

        vm.loadMore()
        advanceUntilIdle()
        assertEquals(ErrorCodes.NETWORK, vm.state.value.loadMoreError?.code)
        assertFalse(vm.state.value.loadingMore)
        vm.loadMore()
        advanceUntilIdle()
        assertEquals(1, repo.pageCalls.size)

        repo.pageError = null
        repo.pages["c1"] = HistoryPage(listOf(at(2)), null)
        vm.retryLoadMore()
        advanceUntilIdle()
        assertNull(vm.state.value.loadMoreError)
        assertEquals(listOf(1L, 2L), ids(vm))
    }

    @Test
    fun `a refresh drops appended pages and a stale load more result`() = runTest(main.dispatcher) {
        repo.firstPage = HistoryPage(listOf(at(1)), "c1")
        repo.pages["c1"] = HistoryPage(listOf(at(2)), null)
        val vm = viewModel()
        advanceUntilIdle()

        val gate = CompletableDeferred<Unit>()
        repo.pageGate = gate
        vm.loadMore()
        advanceUntilIdle()
        assertTrue(vm.state.value.loadingMore)

        repo.cache.value = HistoryPage(listOf(at(5), at(1)), "c9")
        advanceUntilIdle()
        gate.complete(Unit)
        advanceUntilIdle()

        assertEquals(listOf(5L, 1L), ids(vm))
        assertFalse(vm.state.value.loadingMore)
        assertFalse(vm.state.value.endReached)
    }
}
