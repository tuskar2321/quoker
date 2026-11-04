package app.quoker.feature.onboarding.invite

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import app.quoker.core.common.QuokerError
import app.quoker.core.common.format.InviteCode
import app.quoker.core.data.repository.AuthRepository
import app.quoker.core.network.QuokerException
import dagger.hilt.android.lifecycle.HiltViewModel
import javax.inject.Inject
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

data class InviteUiState(
    /** Bare upper-case characters without the dash (the field shows the dash visually). */
    val code: String = "",
    val submitting: Boolean = false,
    val error: QuokerError? = null,
) {
    val canSubmit: Boolean get() = InviteCode.isComplete(code) && !submitting
}

sealed interface InviteEvent {
    data class SignedIn(val telegramLinked: Boolean) : InviteEvent
}

@HiltViewModel
class InviteViewModel @Inject constructor(
    private val auth: AuthRepository,
    private val deviceName: DeviceNameProvider,
) : ViewModel() {
    private val _state = MutableStateFlow(InviteUiState())
    val state: StateFlow<InviteUiState> = _state.asStateFlow()

    private val _events = Channel<InviteEvent>(Channel.BUFFERED)
    val events: Flow<InviteEvent> = _events.receiveAsFlow()

    fun onCodeChange(input: String) {
        _state.update { if (it.submitting) it else it.copy(code = InviteCode.normalize(input), error = null) }
    }

    fun onSubmit() {
        val current = _state.value
        if (!current.canSubmit) return
        _state.update { it.copy(submitting = true, error = null) }
        viewModelScope.launch {
            try {
                val me = auth.register(InviteCode.format(current.code), deviceName.deviceName())
                // Stay in the submitting state: the screen is left right away.
                _events.send(InviteEvent.SignedIn(me.telegram.linked))
            } catch (e: QuokerException) {
                _state.update { it.copy(submitting = false, error = e.error) }
            }
        }
    }
}
