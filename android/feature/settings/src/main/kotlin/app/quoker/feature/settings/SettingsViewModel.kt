package app.quoker.feature.settings

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import app.quoker.core.common.AppInfo
import app.quoker.core.common.QuokerError
import app.quoker.core.data.model.Me
import app.quoker.core.data.model.Target
import app.quoker.core.data.model.TelegramAccount
import app.quoker.core.data.repository.AuthRepository
import app.quoker.core.data.repository.TargetsRepository
import app.quoker.core.network.QuokerException
import app.quoker.feature.settings.ui.UiMessage
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlin.time.Duration
import kotlin.time.Duration.Companion.seconds
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/** A target row: [visible] already reflects a toggle that is still being saved. */
data class TargetRow(val target: Target, val visible: Boolean, val saving: Boolean)

enum class SettingsDialog { Unlink, Logout, HowToAdd }

data class SettingsUiState(
    val me: Me? = null,
    /** Error of the last `me` load when nothing is shown yet. */
    val meError: QuokerError? = null,
    val targets: List<TargetRow> = emptyList(),
    /** The core answered once (or the cache had rows): an empty list is really empty. */
    val targetsLoaded: Boolean = false,
    val refreshingTargets: Boolean = false,
    val unlinking: Boolean = false,
    val signingOut: Boolean = false,
    val dialog: SettingsDialog? = null,
    val message: UiMessage? = null,
    val versionName: String = "",
) {
    val isAdmin: Boolean get() = me?.isAdmin == true
    val telegram: TelegramAccount? get() = me?.telegram
}

sealed interface SettingsEvent {
    data object SignedOut : SettingsEvent
}

@HiltViewModel
class SettingsViewModel internal constructor(
    private val auth: AuthRepository,
    private val targets: TargetsRepository,
    appInfo: AppInfo,
    /** The core re-checks rights in the background after `refresh`; fetch again after this. */
    private val recheckDelay: Duration,
) : ViewModel() {
    @Inject
    constructor(auth: AuthRepository, targets: TargetsRepository, appInfo: AppInfo) :
        this(auth, targets, appInfo, RECHECK_DELAY)

    private val _state = MutableStateFlow(SettingsUiState(versionName = appInfo.versionName))
    val state: StateFlow<SettingsUiState> = _state.asStateFlow()

    private val _events = Channel<SettingsEvent>(Channel.BUFFERED)
    val events: Flow<SettingsEvent> = _events.receiveAsFlow()

    private var cached: List<Target> = emptyList()
    /** Target id → requested visibility while PATCH is in flight. */
    private val pending = mutableMapOf<Long, Boolean>()
    private var resumedOnce = false

    init {
        viewModelScope.launch {
            targets.observe(includeHidden = true).collect { list ->
                cached = list
                publishTargets(loaded = list.isNotEmpty())
            }
        }
        load()
    }

    /** Back from the Telegram link screen or the app: the account and targets may have changed. */
    fun onResume() {
        if (!resumedOnce) {
            resumedOnce = true
            return
        }
        load()
    }

    fun retry() = load()

    private fun load() {
        viewModelScope.launch {
            try {
                val me = auth.me()
                _state.update { it.copy(me = me, meError = null) }
            } catch (e: QuokerException) {
                _state.update { if (it.me == null) it.copy(meError = e.error) else it.copy(message = UiMessage.Failure(e.error)) }
            }
        }
        viewModelScope.launch { fetchTargets(reportErrors = false) }
    }

    private suspend fun fetchTargets(reportErrors: Boolean) {
        try {
            targets.fetch()
            publishTargets(loaded = true)
        } catch (e: QuokerException) {
            if (reportErrors) _state.update { it.copy(message = UiMessage.Failure(e.error)) }
        }
    }

    fun refreshTargets() {
        if (_state.value.refreshingTargets) return
        _state.update { it.copy(refreshingTargets = true) }
        viewModelScope.launch {
            try {
                targets.refresh()
                fetchTargets(reportErrors = true)
                _state.update { it.copy(message = UiMessage.Info(R.string.settings_targets_refresh_queued)) }
                delay(recheckDelay)
                fetchTargets(reportErrors = false)
            } catch (e: QuokerException) {
                _state.update { it.copy(message = UiMessage.Failure(e.error)) }
            } finally {
                _state.update { it.copy(refreshingTargets = false) }
            }
        }
    }

    fun setVisible(targetId: Long, visible: Boolean) {
        if (targetId in pending) return
        pending[targetId] = visible
        publishTargets()
        viewModelScope.launch {
            try {
                targets.setHidden(targetId, hidden = !visible)
            } catch (e: QuokerException) {
                _state.update { it.copy(message = UiMessage.Failure(e.error)) }
            } finally {
                pending.remove(targetId)
                publishTargets()
            }
        }
    }

    fun showDialog(dialog: SettingsDialog) = _state.update { it.copy(dialog = dialog) }

    fun dismissDialog() = _state.update { it.copy(dialog = null) }

    fun dismissMessage() = _state.update { it.copy(message = null) }

    fun unlinkTelegram() {
        if (_state.value.unlinking) return
        _state.update { it.copy(dialog = null, unlinking = true) }
        viewModelScope.launch {
            try {
                auth.unlinkTelegram()
                _state.update { s ->
                    s.copy(me = s.me?.copy(telegram = TelegramAccount(linked = false, username = null, firstName = null)))
                }
                // Unlinking makes every target inactive (user_left).
                fetchTargets(reportErrors = false)
            } catch (e: QuokerException) {
                _state.update { it.copy(message = UiMessage.Failure(e.error)) }
            } finally {
                _state.update { it.copy(unlinking = false) }
            }
        }
    }

    fun logout() {
        if (_state.value.signingOut) return
        _state.update { it.copy(dialog = null, signingOut = true) }
        viewModelScope.launch {
            try {
                auth.logout()
            } finally {
                _state.update { it.copy(signingOut = false) }
                _events.send(SettingsEvent.SignedOut)
            }
        }
    }

    private fun publishTargets(loaded: Boolean = false) {
        val rows = cached.map { t ->
            val requested = pending[t.id]
            TargetRow(target = t, visible = requested ?: !t.hidden, saving = requested != null)
        }
        _state.update { it.copy(targets = rows, targetsLoaded = it.targetsLoaded || loaded) }
    }

    companion object {
        val RECHECK_DELAY: Duration = 4.seconds
    }
}
