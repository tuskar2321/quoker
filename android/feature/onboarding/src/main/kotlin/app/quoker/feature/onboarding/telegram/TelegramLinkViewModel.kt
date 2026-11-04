package app.quoker.feature.onboarding.telegram

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import app.quoker.core.common.Clock
import app.quoker.core.common.QuokerError
import app.quoker.core.data.model.TelegramLink
import app.quoker.core.data.model.TelegramLinkState
import app.quoker.core.data.repository.AuthRepository
import app.quoker.core.network.QuokerException
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.Job
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

enum class TelegramLinkPhase {
    /** No link created yet. */
    Idle,

    /** The deep link was opened; polling the status while the screen is visible. */
    Waiting,

    /** The link expired before the user pressed «Start»; a new one is needed. */
    Expired,
}

data class TelegramLinkUiState(
    val phase: TelegramLinkPhase = TelegramLinkPhase.Idle,
    val creatingLink: Boolean = false,
    val error: QuokerError? = null,
    /** Neither Telegram nor a browser could open the deep link. */
    val openFailed: Boolean = false,
)

sealed interface TelegramLinkEvent {
    data class OpenLink(val url: String) : TelegramLinkEvent
    data object Linked : TelegramLinkEvent
}

@HiltViewModel
class TelegramLinkViewModel @Inject constructor(
    private val auth: AuthRepository,
    private val clock: Clock,
) : ViewModel() {
    private val _state = MutableStateFlow(TelegramLinkUiState())
    val state: StateFlow<TelegramLinkUiState> = _state.asStateFlow()

    private val _events = Channel<TelegramLinkEvent>(Channel.BUFFERED)
    val events: Flow<TelegramLinkEvent> = _events.receiveAsFlow()

    private var link: TelegramLink? = null
    private var visible = false
    private var pollJob: Job? = null
    private var createJob: Job? = null

    /** «Открыть бота в Telegram»: reopens a still valid link, otherwise creates a new one. */
    fun onOpenBot() {
        val current = link
        if (_state.value.phase == TelegramLinkPhase.Waiting && current != null && !current.isExpired()) {
            _state.update { it.copy(openFailed = false) }
            _events.trySend(TelegramLinkEvent.OpenLink(current.deepLink))
            startPolling()
        } else {
            createLink()
        }
    }

    /** «Ссылка истекла — получить новую». */
    fun onRenew() = createLink()

    /** Retry after an error: resume polling or try to create the link again. */
    fun onRetry() {
        _state.update { it.copy(error = null) }
        if (_state.value.phase == TelegramLinkPhase.Waiting) startPolling() else createLink()
    }

    fun onOpenFailed() {
        _state.update { it.copy(openFailed = true) }
    }

    fun onVisible() {
        visible = true
        startPolling()
    }

    fun onHidden() {
        visible = false
        pollJob?.cancel()
        pollJob = null
    }

    private fun createLink() {
        if (createJob?.isActive == true) return
        _state.update { it.copy(creatingLink = true, error = null, openFailed = false) }
        createJob = viewModelScope.launch {
            try {
                val created = auth.createTelegramLink()
                link = created
                _state.update { it.copy(phase = TelegramLinkPhase.Waiting, creatingLink = false) }
                _events.send(TelegramLinkEvent.OpenLink(created.deepLink))
                pollJob?.cancel()
                pollJob = null
                startPolling()
            } catch (e: QuokerException) {
                _state.update { it.copy(creatingLink = false, error = e.error) }
            }
        }
    }

    private fun startPolling() {
        if (!visible || _state.value.phase != TelegramLinkPhase.Waiting || _state.value.error != null) return
        if (pollJob?.isActive == true) return
        pollJob = viewModelScope.launch {
            try {
                auth.pollTelegramStatus().collect { status ->
                    when (status.state) {
                        TelegramLinkState.LINKED -> _events.send(TelegramLinkEvent.Linked)
                        TelegramLinkState.EXPIRED -> {
                            link = null
                            _state.update { it.copy(phase = TelegramLinkPhase.Expired) }
                        }
                        TelegramLinkState.PENDING -> Unit
                    }
                }
            } catch (e: QuokerException) {
                _state.update { it.copy(error = e.error) }
            }
        }
    }

    private fun TelegramLink.isExpired(): Boolean = expiresAt?.let { !clock.now().isBefore(it) } ?: false
}
