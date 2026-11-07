package app.quoker.feature.video

import app.cash.turbine.test
import app.quoker.core.common.QuokerError
import app.quoker.core.data.model.Platform
import app.quoker.core.data.model.ResolveResult
import app.quoker.core.data.model.Send
import app.quoker.core.data.model.SendStatus
import app.quoker.core.data.model.Task
import app.quoker.core.data.model.TaskKind
import app.quoker.core.data.model.TaskStatus
import app.quoker.core.data.repository.SendTracker
import app.quoker.core.data.repository.TrackedSend
import app.quoker.core.download.DownloadErrors
import app.quoker.core.download.DownloadState
import app.quoker.core.download.VideoDownloads
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import kotlinx.coroutines.test.setMain
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

@OptIn(ExperimentalCoroutinesApi::class)
class VideoSheetViewModelTest {
    private val dispatcher = UnconfinedTestDispatcher()
    private val videos = FakeVideoRepository()
    private val auth = FakeAuthRepository()
    private val downloads = FakeVideoDownloads()
    private val tracker = SendTracker()
    private val sends = FakeSendsRepository()

    @Before
    fun setUp() = Dispatchers.setMain(dispatcher)

    @After
    fun tearDown() = Dispatchers.resetMain()

    private fun vm(source: VideoSource = VideoSource.Url("Смотри https://vm.tiktok.com/ZM1abc/ !")) =
        VideoSheetViewModel(source, videos, auth, downloads, tracker, sends)

    /** Keeps [VideoSheetViewModel.state] subscribed like the visible sheet does. */
    private fun TestScope.visible(vm: VideoSheetViewModel) = backgroundScope.launch { vm.state.collect {} }

    @Test
    fun resolvesSharedTextToReady() = runTest(dispatcher) {
        val vm = vm()
        visible(vm)
        assertEquals(VideoContent.Ready(video()), vm.state.value.content)
        assertEquals(1, videos.resolvedUrls.size)
        assertTrue(videos.resolvedUrls.single().contains("ZM1abc"))
    }

    @Test
    fun pendingResolvePollsTask() = runTest(dispatcher) {
        videos.resolveResult = ResolveResult.Pending("t1")
        videos.tasks = listOf(
            Task("t1", TaskKind.RESOLVE, TaskStatus.RUNNING, 40, null, null),
            Task("t1", TaskKind.RESOLVE, TaskStatus.DONE, 100, video(), null),
        )
        val vm = vm()
        visible(vm)
        assertEquals(VideoContent.Ready(video()), vm.state.value.content)
    }

    @Test
    fun resolveErrors() = runTest(dispatcher) {
        data class Case(val name: String, val source: VideoSource, val setup: () -> Unit, val code: String, val platform: Platform?)
        val privateVideo = QuokerError("private_video", "m", false)
        val cases = listOf(
            Case("unsupported link", VideoSource.Url("https://youtube.com/watch?v=1"), {}, "unsupported_url", null),
            Case("server error", VideoSource.Url("https://vm.tiktok.com/ZM1/"), { videos.resolveResult = privateVideo }, "private_video", Platform.TIKTOK),
            Case(
                "failed task",
                VideoSource.Url("https://vm.tiktok.com/ZM1/"),
                {
                    videos.resolveResult = ResolveResult.Pending("t")
                    videos.tasks = listOf(Task("t", TaskKind.RESOLVE, TaskStatus.FAILED, null, null, privateVideo))
                },
                "private_video",
                Platform.TIKTOK,
            ),
            Case("by id", VideoSource.Id("tiktok:1"), { videos.getResult = QuokerError("not_found", "", false) }, "not_found", null),
        )
        for (case in cases) {
            case.setup()
            val vm = vm(case.source)
            val content = vm.state.first { it.content !is VideoContent.Loading }.content as VideoContent.Error
            assertEquals(case.name, case.code, content.error.code)
            assertEquals(case.name, case.platform, content.platform)
        }
    }

    @Test
    fun retryResolvesAgain() = runTest(dispatcher) {
        videos.resolveResult = QuokerError("network", "", true)
        val vm = vm()
        visible(vm)
        assertTrue(vm.state.value.content is VideoContent.Error)
        videos.resolveResult = ResolveResult.Ready(video())
        vm.onRetry()
        assertEquals(VideoContent.Ready(video()), vm.state.value.content)
    }

    @Test
    fun downloadStartsAndFollowsState() = runTest(dispatcher) {
        val vm = vm(VideoSource.Id("tiktok:1"))
        visible(vm)
        vm.onDownloadClick()
        assertEquals(listOf("tiktok:1" to "@a"), downloads.started)
        assertEquals(DownloadState.Preparing, vm.state.value.download)
        downloads.stateOf("tiktok:1").value = DownloadState.Downloading(62)
        assertEquals(DownloadState.Downloading(62), vm.state.value.download)
        vm.onCancelDownload()
        assertEquals(listOf("tiktok:1"), downloads.cancelled)
        assertEquals(DownloadState.Idle, vm.state.value.download)
    }

    @Test
    fun firstDownloadAsksForPermissions() = runTest(dispatcher) {
        downloads.notificationPermissionNeeded = true
        downloads.storagePermissionNeeded = true
        val vm = vm(VideoSource.Id("tiktok:1"))
        visible(vm)
        vm.eventFlow.test {
            vm.onDownloadClick()
            assertEquals(
                VideoSheetEvent.RequestPermissions(listOf(VideoDownloads.STORAGE_PERMISSION, VideoDownloads.NOTIFICATION_PERMISSION)),
                awaitItem(),
            )
        }
        assertTrue(downloads.started.isEmpty())

        // Storage denied: no download, an explanation instead.
        vm.onPermissionsResult(mapOf(VideoDownloads.STORAGE_PERMISSION to false, VideoDownloads.NOTIFICATION_PERMISSION to false))
        assertTrue(downloads.notificationsAsked)
        assertEquals(DownloadState.Failed(DownloadErrors.storagePermission), vm.state.value.download)
        assertTrue(downloads.started.isEmpty())

        // Granted later: the download starts; notifications are not asked again.
        downloads.storagePermissionNeeded = false
        vm.onDownloadClick()
        assertEquals(1, downloads.started.size)
        assertEquals(DownloadState.Preparing, vm.state.value.download)
    }

    @Test
    fun sendOpensTargetsWhenTelegramLinked() = runTest(dispatcher) {
        val vm = vm(VideoSource.Id("tiktok:1"))
        visible(vm)
        vm.eventFlow.test {
            vm.onSendClick()
            assertEquals(VideoSheetEvent.OpenSend("tiktok:1"), awaitItem())
        }
    }

    @Test
    fun sendWithoutTelegramOpensLinking() = runTest(dispatcher) {
        auth.linked = false
        val vm = vm(VideoSource.Id("tiktok:1"))
        visible(vm)
        vm.eventFlow.test {
            vm.onSendClick()
            assertEquals(VideoSheetEvent.LinkTelegram, awaitItem())
            // Linked meanwhile: re-checked on the next tap.
            auth.linked = true
            vm.onSendClick()
            assertEquals(VideoSheetEvent.OpenSend("tiktok:1"), awaitItem())
        }
    }

    @Test
    fun unknownTelegramStateFallsBackToSend() = runTest(dispatcher) {
        auth.linked = null
        val vm = vm(VideoSource.Id("tiktok:1"))
        visible(vm)
        vm.eventFlow.test {
            vm.onSendClick()
            assertEquals(VideoSheetEvent.OpenSend("tiktok:1"), awaitItem())
        }
    }

    @Test
    fun trackedSendIsPolledWhileVisible() = runTest(dispatcher) {
        val vm = vm(VideoSource.Id("tiktok:1"))
        val subscription = visible(vm)
        assertEquals(SendUi.Idle, vm.state.value.send)

        tracker.register(TrackedSend(sendId = 7, videoId = "tiktok:1", targetId = 42, targetTitle = "Друзья"))
        assertEquals(SendUi.Sending("Друзья"), vm.state.value.send)
        assertEquals(1, sends.activePolls)

        sends.pollOf(7).emit(send(SendStatus.SENT))
        assertEquals(SendUi.Sent("Друзья"), vm.state.value.send)

        subscription.cancel()
        advanceTimeBy(6_000)
        runCurrent()
        assertEquals(0, sends.activePolls)
    }

    @Test
    fun failedSendShowsRetry() = runTest(dispatcher) {
        val vm = vm(VideoSource.Id("tiktok:1"))
        visible(vm)
        tracker.register(TrackedSend(8, "tiktok:1", 42, "Канал"))
        val error = QuokerError("target_inactive", "", false)
        sends.pollOf(8).emit(send(SendStatus.FAILED, error).copy(id = 8))
        assertEquals(SendUi.Failed("Канал", error, sendId = 8), vm.state.value.send)
        vm.eventFlow.test {
            vm.onRetrySend(8)
            assertEquals(VideoSheetEvent.OpenSend("tiktok:1", retrySendId = 8), awaitItem())
            vm.onSendAgain()
            assertEquals(VideoSheetEvent.OpenSend("tiktok:1"), awaitItem())
        }
    }

    @Test
    fun displayUrlDropsSchemeAndWww() {
        assertEquals("vm.tiktok.com/ZM1", displayUrl("https://vm.tiktok.com/ZM1/"))
        assertEquals("instagram.com/reel/abc", displayUrl("https://www.instagram.com/reel/abc/"))
    }

    private fun send(status: SendStatus, error: QuokerError? = null) =
        Send(7, "tiktok:1", 42, status, error, null, null)

    @Test
    fun downloadRequestedFromHistory() = runTest(dispatcher) {
        data class Case(val name: String, val initial: DownloadState, val prompt: Boolean, val started: Int)
        val cases = listOf(
            Case("not saved: downloads at once", DownloadState.Idle, prompt = false, started = 1),
            Case("failed before: downloads at once", DownloadState.Failed(QuokerError("network", "", true)), prompt = false, started = 1),
            Case("saved: asks first", DownloadState.Saved("content://media/1"), prompt = true, started = 0),
            Case("running: nothing new", DownloadState.Downloading(40), prompt = false, started = 0),
        )
        for (case in cases) {
            val downloads = FakeVideoDownloads()
            downloads.stateOf(video().id).value = case.initial
            val vm = VideoSheetViewModel(VideoSource.Id(video().id), videos, auth, downloads, tracker, sends)
            visible(vm)
            vm.onDownloadRequested()
            vm.onDownloadRequested() // Recomposition must not repeat the request.
            assertEquals(case.name, case.prompt, vm.state.value.confirmRedownload)
            assertEquals(case.name, case.started, downloads.started.size)
        }
    }

    @Test
    fun redownloadPromptAnswers() = runTest(dispatcher) {
        for (confirm in listOf(true, false)) {
            val downloads = FakeVideoDownloads()
            downloads.stateOf(video().id).value = DownloadState.Saved("content://media/1")
            val vm = VideoSheetViewModel(VideoSource.Id(video().id), videos, auth, downloads, tracker, sends)
            visible(vm)
            vm.onDownloadRequested()
            if (confirm) vm.onConfirmRedownload() else vm.onDismissRedownload()
            assertFalse("confirm=$confirm", vm.state.value.confirmRedownload)
            assertEquals("confirm=$confirm", if (confirm) 1 else 0, downloads.started.size)
        }
    }

    @Test
    fun reshownSheetStartsFresh() = runTest(dispatcher) {
        val downloads = FakeVideoDownloads()
        downloads.stateOf(video().id).value = DownloadState.Saved("content://media/1")
        val vm = VideoSheetViewModel(VideoSource.Id(video().id), videos, auth, downloads, tracker, sends)
        visible(vm)
        vm.onShown()
        vm.onDownloadRequested()
        assertTrue(vm.state.value.confirmRedownload)

        // Closed without answering, then «Скачать снова» again: asks again instead of doing nothing.
        vm.onShown()
        assertFalse(vm.state.value.confirmRedownload)
        vm.onDownloadRequested()
        assertTrue(vm.state.value.confirmRedownload)
        val observedBefore = downloads.observeCalls
        vm.onShown()
        assertTrue("re-observes the saved file", downloads.observeCalls > observedBefore)
    }
}
