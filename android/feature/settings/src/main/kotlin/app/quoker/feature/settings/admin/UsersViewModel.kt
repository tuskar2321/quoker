package app.quoker.feature.settings.admin

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import app.quoker.core.common.Clock
import app.quoker.core.common.QuokerError
import app.quoker.core.data.model.AdminUser
import app.quoker.core.data.model.UserStatus
import app.quoker.core.data.repository.AdminRepository
import app.quoker.core.data.repository.AuthRepository
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

/** Block or unblock awaiting confirmation. */
data class UserAction(val user: AdminUser, val block: Boolean)

data class UsersUiState(
    /** Own account first, then the core's order. Null until the first load. */
    val users: List<AdminUser>? = null,
    val myId: Long? = null,
    val error: QuokerError? = null,
    val confirm: UserAction? = null,
    val busyId: Long? = null,
    val message: UiMessage? = null,
    val now: Instant = Instant.EPOCH,
) {
    val total: Int get() = users.orEmpty().size
    val blocked: Int get() = users.orEmpty().count { it.status == UserStatus.BLOCKED }
    val active: Int get() = total - blocked
}

@HiltViewModel
class UsersViewModel @Inject constructor(
    private val admin: AdminRepository,
    private val auth: AuthRepository,
    private val clock: Clock,
) : ViewModel() {
    private val _state = MutableStateFlow(UsersUiState(now = clock.now()))
    val state: StateFlow<UsersUiState> = _state.asStateFlow()

    init {
        load()
    }

    fun retry() = load()

    private fun load() {
        viewModelScope.launch {
            val myId = _state.value.myId ?: try {
                auth.me().id
            } catch (_: QuokerException) {
                null
            }
            try {
                val users = admin.users()
                _state.update {
                    it.copy(
                        users = users.sortedByDescending { u -> u.id == myId },
                        myId = myId,
                        error = null,
                        now = clock.now(),
                    )
                }
            } catch (e: QuokerException) {
                _state.update { if (it.users == null) it.copy(error = e.error) else it.copy(message = UiMessage.Failure(e.error)) }
            }
        }
    }

    fun ask(user: AdminUser) {
        if (user.id == _state.value.myId) return
        _state.update { it.copy(confirm = UserAction(user, block = user.status != UserStatus.BLOCKED)) }
    }

    fun dismissConfirm() = _state.update { it.copy(confirm = null) }

    fun confirm() {
        val action = _state.value.confirm ?: return
        _state.update { it.copy(confirm = null, busyId = action.user.id) }
        viewModelScope.launch {
            try {
                if (action.block) admin.blockUser(action.user.id) else admin.unblockUser(action.user.id)
                val status = if (action.block) UserStatus.BLOCKED else UserStatus.ACTIVE
                _state.update { s ->
                    s.copy(users = s.users?.map { if (it.id == action.user.id) it.copy(status = status) else it })
                }
            } catch (e: QuokerException) {
                _state.update { it.copy(message = UiMessage.Failure(e.error)) }
            } finally {
                _state.update { it.copy(busyId = null) }
            }
        }
    }

    fun dismissMessage() = _state.update { it.copy(message = null) }
}
