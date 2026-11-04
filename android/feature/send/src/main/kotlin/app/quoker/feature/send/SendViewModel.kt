package app.quoker.feature.send

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import app.quoker.core.data.model.Target
import app.quoker.core.data.repository.AuthRepository
import app.quoker.core.data.repository.SendTracker
import app.quoker.core.data.repository.SendsRepository
import app.quoker.core.data.repository.TargetsRepository
import app.quoker.core.data.repository.TrackedSend
import app.quoker.core.data.repository.VideoRepository
import app.quoker.core.network.ErrorCodes
import app.quoker.core.network.QuokerException
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlin.time.Duration.Companion.seconds
import kotlinx.coroutines.Job
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/**
 * State of the "Куда отправить" sheet. One instance per video; every opening of the sheet is a
 * session ([open]) with its own Idempotency-Key, reused for every press of «Отправить».
 */
@HiltViewModel
class SendViewModel @Inject constructor(
    private val targets: TargetsRepository,
    private val sends: SendsRepository,
    private val tracker: SendTracker,
    private val auth: AuthRepository,
    private val videos: VideoRepository,
) : ViewModel() {
    private val _state = MutableStateFlow(SendUiState())
    val state: StateFlow<SendUiState> = _state.asStateFlow()

    private val _events = Channel<SendEvent>(Channel.BUFFERED)
    val events: Flow<SendEvent> = _events.receiveAsFlow()

    private var videoId: String = ""
    private var idempotencyKey: String = ""
    private var sessionJob: Job? = null
    private var refreshJob: Job? = null

    /** Visible for tests. */
    internal val currentIdempotencyKey: String get() = idempotencyKey

    /**
     * Starts a session for [sessionId] (a new opening of the sheet) or resumes it (same id, e.g.
     * after the composition was recreated): only a new session resets the form and the key.
     */
    fun open(videoId: String, sessionId: String) {
        val resume = sessionId == _state.value.sessionId && videoId == this.videoId
        if (resume && sessionJob?.isActive == true) return
        if (!resume) {
            this.videoId = videoId
            idempotencyKey = sends.newIdempotencyKey()
            refreshJob?.cancel()
            _state.value = SendUiState(sessionId = sessionId)
        }
        sessionJob?.cancel()
        sessionJob = viewModelScope.launch {
            launch { targets.observe(includeHidden = false).collect(::onTargets) }
            launch { fetch() }
            if (_state.value.signName == null) launch { loadSignName() }
            if (!resume) launch { loadDefaultCaption(videoId) }
        }
    }

    /** Stops observing while the sheet is not shown; the session can be resumed with [open]. */
    fun close() {
        sessionJob?.cancel()
        sessionJob = null
    }

    fun select(targetId: Long) {
        _state.update { s ->
            if (s.targets.any { it.id == targetId && it.enabled }) s.copy(selectedId = targetId, sendError = null) else s
        }
    }

    fun onCaptionChange(text: String) {
        _state.update { it.copy(captionText = limitCaption(text), captionTouched = true) }
    }

    fun onSignAsUserChange(checked: Boolean) {
        _state.update { it.copy(signAsUser = checked) }
    }

    fun retryFetch() {
        _state.update { it.copy(fetchError = null) }
        viewModelScope.launch { fetch() }
    }

    /** «Обновить список»: asks the core to re-check rights, then fetches (and once more later). */
    fun refreshList() {
        if (_state.value.refreshing) return
        refreshJob?.cancel()
        refreshJob = viewModelScope.launch {
            _state.update { it.copy(refreshing = true, fetchError = null) }
            try {
                targets.refresh()
            } catch (e: QuokerException) {
                // Rate limited: a recent re-check is still good enough, fetch anyway.
                if (e.code != ErrorCodes.RATE_LIMITED) {
                    _state.update { it.copy(refreshing = false, fetchError = e.error) }
                    return@launch
                }
            }
            fetch()
            _state.update { it.copy(refreshing = false) }
            // The re-check finishes in the background on the core.
            delay(REFETCH_DELAY)
            fetch(silent = true)
        }
    }

    fun send() {
        val s = _state.value
        val target = s.selected ?: return
        if (s.sending) return
        _state.update { it.copy(sending = true, sendError = null) }
        viewModelScope.launch {
            try {
                val send = sends.create(
                    videoId = videoId,
                    targetId = target.id,
                    caption = s.captionToSend,
                    signAsUser = s.signAsUser && s.showSignToggle,
                    idempotencyKey = idempotencyKey,
                )
                tracker.register(TrackedSend(send.id, videoId, target.id, target.target.title))
                _state.update { it.copy(sending = false) }
                _events.send(SendEvent.Sent)
            } catch (e: QuokerException) {
                if (e.code == ErrorCodes.TELEGRAM_NOT_LINKED) {
                    _state.update { it.copy(sending = false) }
                    _events.send(SendEvent.LinkTelegram)
                    return@launch
                }
                _state.update { it.copy(sending = false, sendError = e.error) }
                if (e.code == ErrorCodes.TARGET_INACTIVE || e.code == ErrorCodes.TARGET_FORBIDDEN) {
                    fetch(silent = true)
                }
            }
        }
    }

    private suspend fun fetch(silent: Boolean = false) {
        try {
            targets.fetch()
            _state.update { it.copy(fetched = true, fetchError = null) }
        } catch (e: QuokerException) {
            if (e.code == ErrorCodes.TELEGRAM_NOT_LINKED) {
                _events.send(SendEvent.LinkTelegram)
            } else if (!silent) {
                _state.update { it.copy(fetchError = e.error) }
            }
        }
    }

    private fun onTargets(list: List<Target>) {
        _state.update { s ->
            val items = orderTargets(list)
            val keep = s.selectedId?.takeIf { id -> items.any { it.id == id && it.enabled } }
            s.copy(targets = items, selectedId = keep ?: defaultSelection(items))
        }
    }

    private suspend fun loadSignName() {
        val telegram = try {
            auth.me().telegram
        } catch (_: QuokerException) {
            return
        }
        val name = telegram.username?.takeIf { it.isNotBlank() }?.let { "@$it" }
            ?: telegram.firstName?.takeIf { it.isNotBlank() }
        _state.update { it.copy(signName = name) }
    }

    private suspend fun loadDefaultCaption(videoId: String) {
        val url = try {
            videos.get(videoId).canonicalUrl
        } catch (_: QuokerException) {
            return
        }
        _state.update { if (it.captionTouched) it else it.copy(defaultCaption = limitCaption(url)) }
    }

    companion object {
        internal val REFETCH_DELAY = 3.seconds
    }
}

/** Active targets first in server order, then inactive ones; the latest used active one is marked. */
internal fun orderTargets(list: List<Target>): List<TargetItem> {
    val visible = list.filter { !it.hidden }
    val lastUsedId = visible.filter { it.isActive && it.lastUsedAt != null }.maxByOrNull { it.lastUsedAt!! }?.id
    val (active, inactive) = visible.partition { it.isActive }
    return (active + inactive).map { TargetItem(it, isLastUsed = it.id == lastUsedId) }
}

internal fun defaultSelection(items: List<TargetItem>): Long? =
    (items.firstOrNull { it.isLastUsed } ?: items.firstOrNull { it.enabled })?.id
