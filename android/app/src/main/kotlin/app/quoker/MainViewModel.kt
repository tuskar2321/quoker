package app.quoker

import androidx.lifecycle.SavedStateHandle
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import app.quoker.core.data.repository.AuthRepository
import app.quoker.core.data.repository.HistoryRepository
import app.quoker.core.network.QuokerException
import app.quoker.core.network.SessionEvent
import app.quoker.core.network.SessionEvents
import app.quoker.feature.video.VideoSource
import app.quoker.navigation.PendingDestination
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.filter
import kotlinx.coroutines.flow.filterIsInstance
import kotlinx.coroutines.flow.launchIn
import kotlinx.coroutines.flow.onEach
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch

/** The sheets MainActivity shows over the NavHost; "Куда отправить" stacks above "Видео". */
data class MainSheets(val video: VideoSource? = null, val sendVideoId: String? = null)

@HiltViewModel
class MainViewModel @Inject constructor(
    private val auth: AuthRepository,
    private val history: HistoryRepository,
    sessionEvents: SessionEvents,
    private val savedState: SavedStateHandle,
) : ViewModel() {
    /** Null until the session is read; the NavHost waits for it to pick the start destination. */
    val signedIn: StateFlow<Boolean?> = auth.signedIn.stateIn(viewModelScope, SharingStarted.Eagerly, null)

    private val _blocked = MutableStateFlow(false)
    val blocked: StateFlow<Boolean> = _blocked.asStateFlow()

    private val _pending = MutableStateFlow<PendingDestination?>(null)
    val pending: StateFlow<PendingDestination?> = _pending.asStateFlow()

    val sheets: StateFlow<MainSheets> = combine(
        savedState.getStateFlow<String?>(KEY_VIDEO_URL, null),
        savedState.getStateFlow<String?>(KEY_VIDEO_ID, null),
        savedState.getStateFlow<String?>(KEY_SEND_ID, null),
    ) { url, id, send ->
        MainSheets(
            video = url?.let(VideoSource::Url) ?: id?.let(VideoSource::Id),
            sendVideoId = send,
        )
    }.stateIn(viewModelScope, SharingStarted.Eagerly, MainSheets())

    init {
        sessionEvents.events
            .filterIsInstance<SessionEvent.Blocked>()
            .filter { signedIn.value == true }
            .onEach { _blocked.value = true }
            .launchIn(viewModelScope)
        signedIn
            .filter { it == false }
            .onEach {
                _blocked.value = false
                closeSheets()
            }
            .launchIn(viewModelScope)
    }

    fun onDestination(destination: PendingDestination?) {
        if (destination != null) _pending.value = destination
    }

    fun consumePending() {
        _pending.value = null
    }

    fun openVideo(source: VideoSource) {
        savedState[KEY_VIDEO_URL] = (source as? VideoSource.Url)?.url
        savedState[KEY_VIDEO_ID] = (source as? VideoSource.Id)?.videoId
    }

    fun closeVideo() {
        if (sheets.value.video != null) refreshHistory()
        savedState[KEY_VIDEO_URL] = null
        savedState[KEY_VIDEO_ID] = null
    }

    fun openSend(videoId: String) {
        savedState[KEY_SEND_ID] = videoId
    }

    fun closeSend() {
        savedState[KEY_SEND_ID] = null
    }

    fun closeSheets() {
        closeVideo()
        closeSend()
    }

    /** The video sheet may have downloaded or sent: refresh «Недавние» and the cached history page. */
    private fun refreshHistory() {
        if (signedIn.value != true) return
        viewModelScope.launch {
            try {
                history.refresh()
            } catch (_: QuokerException) {
                // The cached page stays; screens refresh again when they start.
            }
        }
    }

    /** «Выйти» on the blocked screen. */
    fun signOut() {
        viewModelScope.launch {
            auth.logout()
            _blocked.value = false
        }
    }

    private companion object {
        const val KEY_VIDEO_URL = "sheet.video.url"
        const val KEY_VIDEO_ID = "sheet.video.id"
        const val KEY_SEND_ID = "sheet.send.videoId"
    }
}
