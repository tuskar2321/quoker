package app.quoker.feature.settings.admin

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import app.quoker.core.common.Clock
import app.quoker.core.common.QuokerError
import app.quoker.core.data.model.Worker
import app.quoker.core.data.model.WorkerStatus
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

data class AllowWorkerForm(
    val id: String = "",
    val fingerprint: String = "",
    val submitting: Boolean = false,
    val error: QuokerError? = null,
) {
    /** Lower-case hex without the `:` / space separators that `openssl -fingerprint` prints. */
    val normalizedFingerprint: String get() = fingerprint.filterNot { it == ':' || it.isWhitespace() }.lowercase()
    val fingerprintValid: Boolean get() = FINGERPRINT.matches(normalizedFingerprint)
    /** Show the format error only once something was typed. */
    val showFingerprintError: Boolean get() = fingerprint.isNotBlank() && !fingerprintValid
    val canSubmit: Boolean get() = id.isNotBlank() && fingerprintValid && !submitting

    private companion object {
        val FINGERPRINT = Regex("^[0-9a-f]{64}$")
    }
}

enum class CertExpiry { Valid, ExpiresSoon, Expired }

/** The core warns the admin 30 days before a worker certificate expires (docs/06); so does the card. */
internal val CERT_WARNING_PERIOD: java.time.Duration = java.time.Duration.ofDays(30)

internal fun certExpiry(notAfter: Instant, now: Instant): CertExpiry = when {
    !notAfter.isAfter(now) -> CertExpiry.Expired
    notAfter.isBefore(now.plus(CERT_WARNING_PERIOD)) -> CertExpiry.ExpiresSoon
    else -> CertExpiry.Valid
}

data class WorkersUiState(
    /** Allowed first (online before offline), then revoked. Null until the first load. */
    val workers: List<Worker>? = null,
    val error: QuokerError? = null,
    val confirmRevoke: Worker? = null,
    val revokingId: String? = null,
    val allowForm: AllowWorkerForm? = null,
    val message: UiMessage? = null,
    val now: Instant = Instant.EPOCH,
)

@HiltViewModel
class WorkersViewModel @Inject constructor(
    private val admin: AdminRepository,
    private val clock: Clock,
) : ViewModel() {
    private val _state = MutableStateFlow(WorkersUiState(now = clock.now()))
    val state: StateFlow<WorkersUiState> = _state.asStateFlow()

    init {
        load()
    }

    fun retry() = load()

    private fun load() {
        viewModelScope.launch {
            try {
                val workers = admin.workers().sortedWith(
                    compareBy<Worker>({ it.status == WorkerStatus.REVOKED }, { !it.connected }, { it.id }),
                )
                _state.update { it.copy(workers = workers, error = null, now = clock.now()) }
            } catch (e: QuokerException) {
                _state.update { if (it.workers == null) it.copy(error = e.error) else it.copy(message = UiMessage.Failure(e.error)) }
            }
        }
    }

    fun askRevoke(worker: Worker) = _state.update { it.copy(confirmRevoke = worker) }

    fun dismissRevoke() = _state.update { it.copy(confirmRevoke = null) }

    fun revoke() {
        val worker = _state.value.confirmRevoke ?: return
        _state.update { it.copy(confirmRevoke = null, revokingId = worker.id) }
        viewModelScope.launch {
            try {
                admin.revokeWorker(worker.id)
                load()
            } catch (e: QuokerException) {
                _state.update { it.copy(message = UiMessage.Failure(e.error)) }
            } finally {
                _state.update { it.copy(revokingId = null) }
            }
        }
    }

    fun openAllow() = _state.update { it.copy(allowForm = AllowWorkerForm()) }

    fun dismissAllow() = _state.update { if (it.allowForm?.submitting == true) it else it.copy(allowForm = null) }

    fun onAllowIdChange(value: String) = updateForm { it.copy(id = value.trim(), error = null) }

    fun onAllowFingerprintChange(value: String) = updateForm { it.copy(fingerprint = value, error = null) }

    fun allow() {
        val form = _state.value.allowForm ?: return
        if (!form.canSubmit) return
        updateForm { it.copy(submitting = true, error = null) }
        viewModelScope.launch {
            try {
                admin.allowWorker(form.id, form.normalizedFingerprint)
                _state.update { it.copy(allowForm = null) }
                load()
            } catch (e: QuokerException) {
                updateForm { it.copy(submitting = false, error = e.error) }
            }
        }
    }

    fun dismissMessage() = _state.update { it.copy(message = null) }

    private fun updateForm(block: (AllowWorkerForm) -> AllowWorkerForm) =
        _state.update { s -> s.allowForm?.let { s.copy(allowForm = block(it)) } ?: s }
}
