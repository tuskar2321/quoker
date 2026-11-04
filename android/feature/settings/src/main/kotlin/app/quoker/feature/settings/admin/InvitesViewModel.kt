package app.quoker.feature.settings.admin

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import app.quoker.core.common.Clock
import app.quoker.core.common.QuokerError
import app.quoker.core.data.model.Invite
import app.quoker.core.data.model.Role
import app.quoker.core.data.repository.AdminRepository
import app.quoker.core.network.QuokerException
import app.quoker.feature.settings.ui.UiMessage
import dagger.hilt.android.lifecycle.HiltViewModel
import java.time.Instant
import javax.inject.Inject
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/** The code just created; the core never shows it again. */
data class NewInvite(val code: String, val role: Role, val expiresAt: Instant?)

data class InvitesUiState(
    val role: Role = Role.USER,
    val days: Int = DEFAULT_DAYS,
    val creating: Boolean = false,
    val created: NewInvite? = null,
    /** Null until the first load. */
    val invites: List<Invite>? = null,
    val error: QuokerError? = null,
    /** User id → display name («@friend») for used invites. */
    val userNames: Map<Long, String> = emptyMap(),
    val confirmRevoke: Invite? = null,
    val revokingId: Long? = null,
    val message: UiMessage? = null,
    val now: Instant = Instant.EPOCH,
) {
    companion object {
        val DAY_OPTIONS = listOf(1, 3, 7, 14, 30)
        const val DEFAULT_DAYS = AdminRepository.DEFAULT_INVITE_DAYS
    }
}

@HiltViewModel
class InvitesViewModel @Inject constructor(
    private val admin: AdminRepository,
    private val clock: Clock,
) : ViewModel() {
    private val _state = MutableStateFlow(InvitesUiState(now = clock.now()))
    val state: StateFlow<InvitesUiState> = _state.asStateFlow()

    init {
        load()
        viewModelScope.launch {
            try {
                val names = admin.users().associate { u ->
                    u.id to (u.telegramUsername?.takeIf { it.isNotBlank() }?.let { "@$it" } ?: u.telegramFirstName.orEmpty())
                }.filterValues { it.isNotBlank() }
                _state.update { it.copy(userNames = names) }
            } catch (_: QuokerException) {
                // Used invites then show «Использован» without a name.
            }
        }
    }

    fun retry() = load()

    private fun load() {
        viewModelScope.launch {
            try {
                val invites = admin.invites()
                _state.update { it.copy(invites = invites, error = null, now = clock.now()) }
            } catch (e: QuokerException) {
                _state.update { if (it.invites == null) it.copy(error = e.error) else it.copy(message = UiMessage.Failure(e.error)) }
            }
        }
    }

    fun selectRole(role: Role) = _state.update { it.copy(role = role) }

    fun selectDays(days: Int) {
        require(days in 1..AdminRepository.MAX_INVITE_DAYS)
        _state.update { it.copy(days = days) }
    }

    fun create() {
        val s = _state.value
        if (s.creating) return
        _state.update { it.copy(creating = true, message = null) }
        viewModelScope.launch {
            try {
                val created = admin.createInvite(s.role, s.days)
                _state.update { it.copy(created = NewInvite(created.code, s.role, created.expiresAt)) }
                load()
            } catch (e: QuokerException) {
                _state.update { it.copy(message = UiMessage.Failure(e.error)) }
            } finally {
                _state.update { it.copy(creating = false) }
            }
        }
    }

    fun askRevoke(invite: Invite) = _state.update { it.copy(confirmRevoke = invite) }

    fun dismissRevoke() = _state.update { it.copy(confirmRevoke = null) }

    fun revoke() {
        val invite = _state.value.confirmRevoke ?: return
        _state.update { it.copy(confirmRevoke = null, revokingId = invite.id) }
        viewModelScope.launch {
            try {
                admin.revokeInvite(invite.id)
                _state.update { s -> s.copy(invites = s.invites?.filterNot { it.id == invite.id }) }
            } catch (e: QuokerException) {
                _state.update { it.copy(message = UiMessage.Failure(e.error)) }
            } finally {
                _state.update { it.copy(revokingId = null) }
            }
        }
    }

    fun dismissMessage() = _state.update { it.copy(message = null) }
}
