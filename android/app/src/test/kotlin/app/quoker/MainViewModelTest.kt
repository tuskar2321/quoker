package app.quoker

import androidx.lifecycle.SavedStateHandle
import app.quoker.core.data.model.HistoryPage
import app.quoker.core.data.model.Me
import app.quoker.core.data.model.TelegramLink
import app.quoker.core.data.model.TelegramLinkStatus
import app.quoker.core.data.repository.AuthRepository
import app.quoker.core.data.repository.HistoryRepository
import app.quoker.core.network.SessionEvent
import app.quoker.core.network.SessionEvents
import app.quoker.feature.video.VideoSource
import app.quoker.navigation.PendingDestination
import kotlin.time.Duration
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class MainViewModelTest {
    private class FakeAuthRepository(signedIn: Boolean?) : AuthRepository {
        val session = MutableStateFlow(signedIn)
        var logouts = 0

        override val signedIn: Flow<Boolean> = session.filterNotNull()

        override suspend fun logout() {
            logouts++
            session.value = false
        }

        override suspend fun register(inviteCode: String, deviceName: String): Me = error("unused")
        override suspend fun me(): Me = error("unused")
        override suspend fun createTelegramLink(): TelegramLink = error("unused")
        override suspend fun telegramStatus(): TelegramLinkStatus = error("unused")
        override fun pollTelegramStatus(interval: Duration): Flow<TelegramLinkStatus> = error("unused")
        override suspend fun unlinkTelegram() = error("unused")
    }

    private class FakeHistoryRepository : HistoryRepository {
        var refreshes = 0
        override val cachedFirstPage: Flow<HistoryPage> = MutableStateFlow(HistoryPage(emptyList(), null))
        override suspend fun refresh(limit: Int): HistoryPage {
            refreshes++
            return HistoryPage(emptyList(), null)
        }
        override suspend fun page(cursor: String, limit: Int): HistoryPage = error("unused")
    }

    private val events = SessionEvents()
    private val history = FakeHistoryRepository()

    @Before
    fun setUp() = Dispatchers.setMain(UnconfinedTestDispatcher())

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun viewModel(auth: AuthRepository, state: SavedStateHandle = SavedStateHandle()) =
        MainViewModel(auth, history, events, state)

    @Test
    fun sessionUnknownUntilRead() = runTest {
        val auth = FakeAuthRepository(signedIn = null)
        val vm = viewModel(auth)
        assertNull(vm.signedIn.value)
        auth.session.value = true
        assertEquals(true, vm.signedIn.value)
    }

    @Test
    fun blockedOnlyWhileSignedIn() = runTest {
        val cases = listOf(true to true, false to false)
        for ((signedIn, expected) in cases) {
            val vm = viewModel(FakeAuthRepository(signedIn))
            events.emit(SessionEvent.Blocked)
            assertEquals("signedIn=$signedIn", expected, vm.blocked.value)
        }
    }

    @Test
    fun signOutFromBlockedClearsSessionAndSheets() = runTest {
        val auth = FakeAuthRepository(signedIn = true)
        val vm = viewModel(auth)
        vm.openVideo(VideoSource.Url("https://www.tiktok.com/@a/video/1"))
        vm.openSend("v1")
        events.emit(SessionEvent.Blocked)
        assertTrue(vm.blocked.value)

        vm.signOut()

        assertEquals(1, auth.logouts)
        assertEquals(false, vm.signedIn.value)
        assertFalse(vm.blocked.value)
        assertEquals(MainSheets(), vm.sheets.value)
    }

    @Test
    fun sheetsSurviveRecreation() = runTest {
        val state = SavedStateHandle()
        val first = viewModel(FakeAuthRepository(true), state)
        first.openVideo(VideoSource.Id("v1"))
        first.openSend("v1")

        val second = viewModel(FakeAuthRepository(true), state)
        assertEquals(MainSheets(VideoSource.Id("v1"), "v1"), second.sheets.value)

        second.closeSend()
        assertEquals(MainSheets(VideoSource.Id("v1"), null), second.sheets.value)
        second.openVideo(VideoSource.Url("u"))
        assertEquals(MainSheets(VideoSource.Url("u"), null), second.sheets.value)
    }

    @Test
    fun retrySendIdTravelsWithTheSendSheetOnly() = runTest {
        val state = SavedStateHandle()
        val first = viewModel(FakeAuthRepository(true), state)
        first.openSend("v1", retrySendId = 7)
        assertEquals(MainSheets(null, "v1", 7), first.sheets.value)

        val second = viewModel(FakeAuthRepository(true), state)
        assertEquals(MainSheets(null, "v1", 7), second.sheets.value)

        second.openSend("v1")
        assertEquals(MainSheets(null, "v1", null), second.sheets.value)
        second.openSend("v2", retrySendId = 8)
        second.closeSend()
        assertEquals(MainSheets(), second.sheets.value)
    }

    @Test
    fun pendingDestinationIsConsumedOnce() = runTest {
        val vm = viewModel(FakeAuthRepository(true))
        vm.onDestination(null)
        assertNull(vm.pending.value)
        vm.onDestination(PendingDestination.TelegramLink)
        assertEquals(PendingDestination.TelegramLink, vm.pending.value)
        vm.consumePending()
        assertNull(vm.pending.value)
    }

    @Test
    fun closingVideoSheetRefreshesHistory() = runTest {
        val vm = viewModel(FakeAuthRepository(true))
        vm.closeSheets()
        assertEquals(0, history.refreshes)
        vm.openVideo(VideoSource.Id("v1"))
        vm.openSend("v1")
        vm.closeSend()
        assertEquals(0, history.refreshes)
        vm.closeSheets()
        assertEquals(1, history.refreshes)
    }
}
