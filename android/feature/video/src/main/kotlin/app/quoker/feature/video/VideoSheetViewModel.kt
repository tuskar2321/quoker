package app.quoker.feature.video

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import app.quoker.core.common.QuokerError
import app.quoker.core.common.link.LinkExtractor
import app.quoker.core.common.link.LinkResult
import app.quoker.core.data.model.Platform
import app.quoker.core.data.model.ResolveResult
import app.quoker.core.data.model.SendStatus
import app.quoker.core.data.model.TaskStatus
import app.quoker.core.data.model.Video
import app.quoker.core.data.repository.AuthRepository
import app.quoker.core.data.repository.SendTracker
import app.quoker.core.data.repository.SendsRepository
import app.quoker.core.data.repository.VideoRepository
import app.quoker.core.download.DownloadErrors
import app.quoker.core.download.DownloadState
import app.quoker.core.download.VideoDownloads
import app.quoker.core.network.ErrorCodes
import app.quoker.core.network.QuokerException
import dagger.assisted.Assisted
import dagger.assisted.AssistedFactory
import dagger.assisted.AssistedInject
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlin.coroutines.cancellation.CancellationException
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.Job
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.catch
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.last
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.onStart
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch

/**
 * State of the "Видео" sheet. Download and send state are collected only while the sheet observes
 * [state] (WhileSubscribed), so the send poll stops when the sheet closes (docs/08 «Отправка»).
 */
@OptIn(ExperimentalCoroutinesApi::class)
@HiltViewModel(assistedFactory = VideoSheetViewModel.Factory::class)
class VideoSheetViewModel @AssistedInject constructor(
    @Assisted private val source: VideoSource,
    private val videos: VideoRepository,
    private val auth: AuthRepository,
    private val downloads: VideoDownloads,
    private val tracker: SendTracker,
    private val sends: SendsRepository,
) : ViewModel() {

    @AssistedFactory
    interface Factory {
        fun create(source: VideoSource): VideoSheetViewModel
    }

    private val content = MutableStateFlow<VideoContent>(VideoContent.Loading)
    private val downloadOverride = MutableStateFlow<DownloadState?>(null)
    private val checkingTelegram = MutableStateFlow(false)
    private val notificationsShown = MutableStateFlow(downloads.notificationsAllowed())
    private var telegramLinked: Boolean? = null
    private var resolveJob: Job? = null

    private val events = Channel<VideoSheetEvent>(Channel.BUFFERED)
    val eventFlow: Flow<VideoSheetEvent> = events.receiveAsFlow()

    private val videoId: Flow<String?> = content
        .map { (it as? VideoContent.Ready)?.video?.id }
        .distinctUntilChanged()

    private val download: Flow<DownloadState> = combine(
        videoId.flatMapLatest { id -> if (id == null) flowOf(DownloadState.Idle) else downloads.observe(id) },
        downloadOverride,
    ) { observed, override -> override ?: observed }

    private val send: Flow<SendUi> = combine(
        videoId.flatMapLatest { id -> if (id == null) flowOf(SendUi.Idle) else sendState(id) },
        checkingTelegram,
    ) { send, checking -> if (checking && send !is SendUi.Sending) SendUi.Checking else send }

    val state: StateFlow<VideoSheetUiState> = combine(content, download, send, notificationsShown) { c, d, s, n ->
        VideoSheetUiState(content = c, download = d, send = s, notificationsShown = n)
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(STOP_TIMEOUT_MS), VideoSheetUiState())

    init {
        load()
        viewModelScope.launch {
            telegramLinked = fetchTelegramLinked()
        }
    }

    fun onRetry() = load()

    fun onDownloadClick() {
        val video = readyVideo() ?: return
        val permissions = buildList {
            if (downloads.needsStoragePermission()) add(VideoDownloads.STORAGE_PERMISSION)
            if (downloads.needsNotificationPermission()) add(VideoDownloads.NOTIFICATION_PERMISSION)
        }
        if (permissions.isNotEmpty()) {
            events.trySend(VideoSheetEvent.RequestPermissions(permissions))
        } else {
            startDownload(video)
        }
    }

    /** Result of [VideoSheetEvent.RequestPermissions]. */
    fun onPermissionsResult(granted: Map<String, Boolean>) {
        if (VideoDownloads.NOTIFICATION_PERMISSION in granted) downloads.onNotificationPermissionAsked()
        notificationsShown.value = downloads.notificationsAllowed()
        val video = readyVideo() ?: return
        if (downloads.needsStoragePermission()) {
            downloadOverride.value = DownloadState.Failed(DownloadErrors.storagePermission)
            return
        }
        startDownload(video)
    }

    fun onCancelDownload() {
        readyVideo()?.let { downloads.cancel(it.id) }
    }

    fun onSendClick() {
        val video = readyVideo() ?: return
        if (telegramLinked == true) {
            events.trySend(VideoSheetEvent.OpenSend(video.id))
            return
        }
        if (checkingTelegram.value) return
        checkingTelegram.value = true
        viewModelScope.launch {
            // Re-check: the user may have linked Telegram since the sheet opened.
            val linked = fetchTelegramLinked() ?: telegramLinked
            telegramLinked = linked
            checkingTelegram.value = false
            // Unknown (network error): let the send sheet report the real problem.
            events.send(if (linked == false) VideoSheetEvent.LinkTelegram else VideoSheetEvent.OpenSend(video.id))
        }
    }

    /** «Ещё» after a sent result and «Повторить» after a failed one: pick a target again. */
    fun onSendAgain() {
        readyVideo()?.let { events.trySend(VideoSheetEvent.OpenSend(it.id)) }
    }

    private fun startDownload(video: Video) {
        downloadOverride.value = null
        downloads.start(video.id, video.author?.let { "@$it" })
    }

    private fun readyVideo(): Video? = (content.value as? VideoContent.Ready)?.video

    private suspend fun fetchTelegramLinked(): Boolean? = try {
        auth.me().telegram.linked
    } catch (e: CancellationException) {
        throw e
    } catch (_: QuokerException) {
        null
    }

    private fun sendState(videoId: String): Flow<SendUi> = tracker.observe(videoId).flatMapLatest { tracked ->
        if (tracked == null) {
            flowOf(SendUi.Idle)
        } else {
            sends.poll(tracked.sendId)
                .map { s ->
                    when (s.status) {
                        SendStatus.SENT -> SendUi.Sent(tracked.targetTitle)
                        SendStatus.FAILED -> SendUi.Failed(tracked.targetTitle, s.error)
                        SendStatus.QUEUED, SendStatus.SENDING -> SendUi.Sending(tracked.targetTitle)
                    }
                }
                .onStart { emit(SendUi.Sending(tracked.targetTitle)) }
                // The poll gave up (network): keep the last known status; history has the result.
                .catch { e -> if (e !is QuokerException) throw e }
        }
    }

    private fun load() {
        resolveJob?.cancel()
        content.value = VideoContent.Loading
        resolveJob = viewModelScope.launch {
            content.value = when (source) {
                is VideoSource.Id -> guard(null, null) { VideoContent.Ready(videos.get(source.videoId)) }
                is VideoSource.Url -> resolveUrl(source.url)
            }
        }
    }

    private suspend fun resolveUrl(text: String): VideoContent {
        val link = when (val parsed = LinkExtractor.extract(text)) {
            is LinkResult.Rejected -> return VideoContent.Error(
                QuokerError(parsed.reason.code, "", retryable = false),
                platform = null,
                url = text.trim().takeIf { it.isNotEmpty() },
            )
            is LinkResult.Supported -> parsed.link
        }
        val platform = when (link.platform) {
            app.quoker.core.common.link.Platform.TikTok -> Platform.TIKTOK
            app.quoker.core.common.link.Platform.Instagram -> Platform.INSTAGRAM
        }
        // The link as the user shared it (short vm.tiktok.com links stay short), not the canonical one.
        val shown = urlRe.findAll(text).map { it.value }.firstOrNull { LinkExtractor.firstSupported(it) == link } ?: link.url
        return guard(platform, shown) {
            when (val result = videos.resolve(link.url)) {
                is ResolveResult.Ready -> VideoContent.Ready(result.video)
                is ResolveResult.Pending -> {
                    val task = videos.pollTask(result.taskId).last()
                    val video = task.video
                    when {
                        task.status == TaskStatus.DONE && video != null -> VideoContent.Ready(video)
                        else -> VideoContent.Error(
                            task.error ?: QuokerError(ErrorCodes.INTERNAL, "", retryable = true),
                            platform,
                            shown,
                        )
                    }
                }
            }
        }
    }

    private inline fun guard(platform: Platform?, url: String?, block: () -> VideoContent): VideoContent = try {
        block()
    } catch (e: CancellationException) {
        throw e
    } catch (e: QuokerException) {
        VideoContent.Error(e.error, platform, url)
    }

    private companion object {
        const val STOP_TIMEOUT_MS = 5_000L
        val urlRe = Regex("""(?i)https?://[^\s<>"'«»]+""")
    }
}
