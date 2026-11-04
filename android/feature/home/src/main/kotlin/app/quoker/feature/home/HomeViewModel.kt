package app.quoker.feature.home

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import app.quoker.core.common.link.Link
import app.quoker.core.common.link.LinkExtractor
import app.quoker.core.common.link.LinkResult
import app.quoker.core.data.model.HistoryActionType
import app.quoker.core.data.model.HistoryItem
import app.quoker.core.data.model.Platform
import app.quoker.core.data.model.WorkerStatus
import app.quoker.core.data.repository.AdminRepository
import app.quoker.core.data.repository.AuthRepository
import app.quoker.core.data.repository.HistoryRepository
import app.quoker.core.network.QuokerException
import dagger.hilt.android.lifecycle.HiltViewModel
import java.time.Instant
import javax.inject.Inject
import kotlin.time.Duration.Companion.seconds
import kotlinx.coroutines.Job
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.catch
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.onStart
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/** A «Недавние» row; [url] is the video's canonical URL passed to onOpenVideo. */
data class RecentItem(
    val id: Long,
    val url: String,
    val coverUrl: String?,
    val author: String?,
    val platform: Platform,
    val downloaded: Boolean,
    /** Distinct target titles, newest first. */
    val sentTo: List<String>,
    /** The latest send attempt failed and nothing was sent since. */
    val sendFailed: Boolean,
    /** Time of the latest action. */
    val at: Instant?,
)

data class HomeUiState(
    val link: String = "",
    /** Error code (unsupported_url / unsupported_content) of the rejected input. */
    val linkError: String? = null,
    /** Supported link from the clipboard that differs from the field: shows the chip. */
    val clipboardLink: Link? = null,
    val recent: List<RecentItem> = emptyList(),
    val offline: Boolean = false,
    /** Admin only: the core reports no connected worker. */
    val noWorkers: Boolean = false,
) {
    val canOpen: Boolean get() = link.isNotBlank()
}

sealed interface HomeEvent {
    data class OpenVideo(val url: String) : HomeEvent
}

@HiltViewModel
class HomeViewModel @Inject constructor(
    private val history: HistoryRepository,
    private val auth: AuthRepository,
    private val admin: AdminRepository,
    network: NetworkMonitor,
) : ViewModel() {
    private data class Input(val link: String = "", val linkError: String? = null, val clipboard: Link? = null)

    private val input = MutableStateFlow(Input())
    private val noWorkers = MutableStateFlow(false)

    private val recent: Flow<List<RecentItem>> = history.cachedFirstPage
        .map { page -> page.items.take(RECENT_COUNT).map { it.toRecent() } }
        .catch { emit(emptyList()) }

    val state: StateFlow<HomeUiState> = combine(
        input,
        recent.onStart { emit(emptyList()) },
        network.isOnline.onStart { emit(true) },
        noWorkers,
    ) { input, recent, online, noWorkers ->
        val fieldKey = LinkExtractor.firstSupported(input.link)?.key
        HomeUiState(
            link = input.link,
            linkError = input.linkError,
            clipboardLink = input.clipboard?.takeIf { it.key != fieldKey },
            recent = recent,
            offline = !online,
            noWorkers = noWorkers,
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), HomeUiState())

    private val _events = Channel<HomeEvent>(Channel.BUFFERED)
    val events: Flow<HomeEvent> = _events.receiveAsFlow()

    private var isAdmin: Boolean? = null
    private var visibleJob: Job? = null

    fun onLinkChange(text: String) {
        input.update { it.copy(link = text, linkError = null) }
    }

    /** «Вставить» in the field: the supported link from the text, else the raw text. */
    fun onPaste(clipboardText: String?) {
        val text = clipboardText?.trim().orEmpty()
        if (text.isEmpty()) return
        val link = LinkExtractor.firstSupported(text)?.url ?: text
        input.update { it.copy(link = link, linkError = null) }
    }

    /** Clipboard contents read when the window gains focus after onResume. */
    fun onClipboardText(text: String?) {
        val link = text?.let(LinkExtractor::firstSupported)
        input.update { it.copy(clipboard = link) }
    }

    fun onClipboardChipClick() {
        val link = input.value.clipboard ?: return
        input.update { it.copy(link = link.url, linkError = null) }
    }

    fun onOpenVideo() {
        val text = input.value.link
        if (text.isBlank()) return
        when (val result = LinkExtractor.extract(text)) {
            is LinkResult.Supported -> _events.trySend(HomeEvent.OpenVideo(result.link.url))
            is LinkResult.Rejected -> input.update { it.copy(linkError = result.reason.code) }
        }
    }

    fun onRecentClick(item: RecentItem) {
        _events.trySend(HomeEvent.OpenVideo(item.url))
    }

    /** Screen started: refresh «Недавние» once and watch the worker status for admins. */
    fun onStart() {
        visibleJob?.cancel()
        visibleJob = viewModelScope.launch {
            launch { refreshHistory() }
            watchWorkers()
        }
    }

    fun onStop() {
        visibleJob?.cancel()
        visibleJob = null
    }

    private suspend fun refreshHistory() {
        try {
            history.refresh()
        } catch (_: QuokerException) {
            // The cached page stays; the no-network banner explains offline failures.
        }
    }

    private suspend fun watchWorkers() {
        while (true) {
            try {
                val admin = isAdmin ?: auth.me().isAdmin.also { isAdmin = it }
                if (!admin) {
                    noWorkers.value = false
                    return
                }
                val status = this.admin.status()
                noWorkers.value = status.workers.none { it.connected && it.status == WorkerStatus.ALLOWED }
            } catch (_: QuokerException) {
                // Keep the last known value.
            }
            delay(WORKER_STATUS_INTERVAL)
        }
    }

    private companion object {
        const val RECENT_COUNT = 3
        val WORKER_STATUS_INTERVAL = 60.seconds
    }
}

internal fun HistoryItem.toRecent(): RecentItem {
    val sent = actions.filter { it.type == HistoryActionType.SENT }.mapNotNull { it.targetTitle }.distinct()
    val latestSendAttempt = actions.firstOrNull { it.type == HistoryActionType.SENT || it.type == HistoryActionType.SEND_FAILED }
    return RecentItem(
        id = id,
        url = video.canonicalUrl.ifBlank { video.sourceUrl },
        coverUrl = video.thumbnailUrl,
        author = video.author?.removePrefix("@")?.takeIf { it.isNotBlank() },
        platform = video.platform,
        downloaded = actions.any { it.type == HistoryActionType.DOWNLOADED },
        sentTo = sent,
        sendFailed = latestSendAttempt?.type == HistoryActionType.SEND_FAILED,
        at = actions.firstNotNullOfOrNull { it.at },
    )
}
