package app.quoker.feature.home

import app.cash.turbine.test
import app.quoker.core.data.model.HistoryActionType
import app.quoker.core.data.model.HistoryPage
import app.quoker.core.data.model.Role
import app.quoker.core.data.model.WorkerStatus
import kotlin.time.Duration.Companion.seconds
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class HomeViewModelTest {
    @get:Rule
    val main = MainDispatcherRule()

    private val history = FakeHistoryRepository()
    private val auth = FakeAuthRepository()
    private val admin = FakeAdminRepository()
    private val network = FakeNetworkMonitor()
    private val now = java.time.Instant.parse("2026-11-05T12:00:00Z")
    private val clock = app.quoker.core.common.Clock { now }
    private val vm by lazy { HomeViewModel(history, auth, admin, network, clock) }

    private fun test(body: suspend TestScope.() -> Unit) = runTest(main.dispatcher) {
        backgroundScope.launch { vm.state.collect {} }
        try {
            body()
        } finally {
            vm.onStop()
        }
    }

    @Test
    fun openVideoValidatesTheLink() = test {
        val cases = listOf(
            "Смотри https://vm.tiktok.com/ZMabc123/ !" to null,
            "https://example.com/video" to "unsupported_url",
            "https://www.instagram.com/stories/someone/123/" to "unsupported_content",
        )
        for ((text, error) in cases) {
            vm.onLinkChange(text)
            vm.events.test {
                vm.onOpenVideo()
                if (error == null) {
                    assertTrue(awaitItem() is HomeEvent.OpenVideo)
                } else {
                    expectNoEvents()
                }
            }
            assertEquals(text, error, vm.state.value.linkError)
        }
    }

    @Test
    fun blankLinkCannotBeOpened() = test {
        assertFalse(vm.state.value.canOpen)
        vm.events.test {
            vm.onOpenVideo()
            expectNoEvents()
        }
    }

    @Test
    fun clipboardChipShownOnlyForSupportedLinkNotInField() = test {
        vm.onClipboardText("hello")
        assertNull(vm.state.value.clipboardLink)
        vm.onClipboardText("look https://vm.tiktok.com/ZMabc123/")
        val link = vm.state.value.clipboardLink!!
        vm.onClipboardChipClick()
        assertEquals(link.url, vm.state.value.link)
        assertNull(vm.state.value.clipboardLink)
        vm.onClipboardText(null)
        assertNull(vm.state.value.clipboardLink)
    }

    @Test
    fun pasteExtractsTheLink() = test {
        vm.onPaste("look https://vm.tiktok.com/ZMabc123/ wow")
        assertTrue(vm.state.value.link.startsWith("https://vm.tiktok.com/ZMabc123"))
        vm.onPaste("just text")
        assertEquals("just text", vm.state.value.link)
        vm.onPaste(null)
        assertEquals("just text", vm.state.value.link)
    }

    @Test
    fun recentShowsThreeLatestAndRefreshesOnStart() = test {
        assertTrue(vm.state.value.recent.isEmpty())
        history.page.value = HistoryPage((1L..5L).map { historyItem(it, action(HistoryActionType.RESOLVED)) }, "c")
        assertEquals(listOf(1L, 2L, 3L), vm.state.value.recent.map { it.id })
        vm.onStart()
        assertEquals(1, history.refreshes)
    }

    @Test
    fun refreshFailureKeepsCache() = test {
        history.page.value = HistoryPage(listOf(historyItem(1, action(HistoryActionType.DOWNLOADED))), null)
        history.refreshFailure = networkError()
        vm.onStart()
        assertEquals(1, vm.state.value.recent.size)
    }

    @Test
    fun recentOpensCanonicalUrl() = test {
        history.page.value = HistoryPage(listOf(historyItem(7, action(HistoryActionType.RESOLVED))), null)
        vm.events.test {
            vm.onRecentClick(vm.state.value.recent.single())
            assertEquals(HomeEvent.OpenVideo("https://www.tiktok.com/@author/video/7"), awaitItem())
        }
    }

    @Test
    fun offlineBanner() = test {
        assertFalse(vm.state.value.offline)
        network.isOnline.value = false
        assertTrue(vm.state.value.offline)
    }

    @Test
    fun noWorkersBannerForAdminOnly() = test {
        val cases = listOf(
            Triple(Role.ADMIN, emptyList(), true),
            Triple(Role.ADMIN, listOf(worker(connected = false)), true),
            Triple(Role.ADMIN, listOf(worker(connected = true, status = WorkerStatus.REVOKED)), true),
            Triple(Role.ADMIN, listOf(worker(connected = false), worker(connected = true)), false),
            Triple(Role.ADMIN, listOf(worker(connected = false, lastSeenAt = now.minusSeconds(120))), false),
            Triple(Role.ADMIN, listOf(worker(connected = false, lastSeenAt = now.minusSeconds(301))), true),
            Triple(Role.USER, emptyList(), false),
        )
        for ((role, workers, expected) in cases) {
            val auth = FakeAuthRepository(role)
            val admin = FakeAdminRepository().also { it.workers = workers }
            val vm = HomeViewModel(history, auth, admin, network, clock)
            backgroundScope.launch { vm.state.collect {} }
            vm.onStart()
            runCurrent()
            assertEquals("$role $workers", expected, vm.state.value.noWorkers)
            if (role == Role.USER) assertEquals(0, admin.statusCalls)
            vm.onStop()
        }
    }

    @Test
    fun adminStatusRefreshesWhileVisible() = test {
        auth.role = Role.ADMIN
        vm.onStart()
        runCurrent()
        assertTrue(vm.state.value.noWorkers)
        admin.workers = listOf(worker(connected = true))
        advanceTimeBy(61.seconds)
        assertFalse(vm.state.value.noWorkers)
        assertEquals(2, admin.statusCalls)
        assertEquals(1, auth.meCalls)
        vm.onStop()
        advanceTimeBy(120.seconds)
        assertEquals(2, admin.statusCalls)
    }
}
