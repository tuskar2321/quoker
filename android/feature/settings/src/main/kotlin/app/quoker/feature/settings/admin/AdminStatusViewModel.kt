package app.quoker.feature.settings.admin

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import app.quoker.core.common.Clock
import app.quoker.core.common.QuokerError
import app.quoker.core.data.model.AdminStatus
import app.quoker.core.data.model.UserStatus
import app.quoker.core.data.model.noWorkersAlert
import app.quoker.core.data.repository.AdminRepository
import app.quoker.core.network.QuokerException
import app.quoker.feature.settings.R
import app.quoker.feature.settings.data.CookiesFile
import app.quoker.feature.settings.data.CookiesFileReader
import app.quoker.feature.settings.data.CookiesUploadStore
import app.quoker.feature.settings.ui.UiMessage
import dagger.hilt.android.lifecycle.HiltViewModel
import java.time.Duration
import java.time.Instant
import javax.inject.Inject
import kotlin.math.roundToInt
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

sealed interface AdminAlert {
    data object NoWorkers : AdminAlert
    data class InstagramErrors(val count: Int) : AdminAlert
    data class DiskAlmostFull(val percent: Int, val cacheBytes: Long) : AdminAlert
}

data class UsersSummary(val total: Int, val blocked: Int)

data class AdminStatusUiState(
    val status: AdminStatus? = null,
    /** Error of the last status load when nothing is shown yet. */
    val error: QuokerError? = null,
    val activeInvites: Int? = null,
    val users: UsersSummary? = null,
    val cookiesUploadedAt: Instant? = null,
    val uploadingCookies: Boolean = false,
    val message: UiMessage? = null,
    val now: Instant = Instant.EPOCH,
) {
    val onlineWorkers get() = status?.workers.orEmpty().filter { it.isOnline }
    val poller get() = onlineWorkers.firstOrNull { it.isPoller }

    val alerts: List<AdminAlert>
        get() {
            val s = status ?: return emptyList()
            return buildList {
                if (s.workers.noWorkersAlert(now)) add(AdminAlert.NoWorkers)
                if (s.errors24h.instagram > 0) add(AdminAlert.InstagramErrors(s.errors24h.instagram))
                val percent = diskPercent(s)
                if (percent > DISK_ALERT_PERCENT) add(AdminAlert.DiskAlmostFull(percent, s.disk.cacheBytes))
            }
        }

    /** Whole days since the last upload from this device, or null when unknown. */
    val cookiesAgeDays: Long? get() = cookiesUploadedAt?.let { Duration.between(it, now).toDays().coerceAtLeast(0) }
    val cookiesStale: Boolean get() = (cookiesAgeDays ?: 0) > COOKIES_STALE_DAYS

    companion object {
        const val DISK_ALERT_PERCENT = 80
        const val COOKIES_STALE_DAYS = 14L

        fun diskPercent(s: AdminStatus): Int = (s.disk.usedFraction * 100).roundToInt().coerceIn(0, 100)
    }
}

@HiltViewModel
class AdminStatusViewModel @Inject constructor(
    private val admin: AdminRepository,
    private val cookiesStore: CookiesUploadStore,
    private val cookiesReader: CookiesFileReader,
    private val clock: Clock,
) : ViewModel() {
    private val _state = MutableStateFlow(AdminStatusUiState(now = clock.now()))
    val state: StateFlow<AdminStatusUiState> = _state.asStateFlow()

    private var resumedOnce = false

    init {
        viewModelScope.launch {
            cookiesStore.lastUploadAt.collect { at -> _state.update { it.copy(cookiesUploadedAt = at) } }
        }
        load()
    }

    /** Back from invites/users/workers: counters may have changed. */
    fun onResume() {
        if (!resumedOnce) {
            resumedOnce = true
            return
        }
        load()
    }

    fun retry() = load()

    private fun load() {
        _state.update { it.copy(now = clock.now()) }
        viewModelScope.launch {
            try {
                val status = admin.status()
                _state.update { it.copy(status = status, error = null, now = clock.now()) }
            } catch (e: QuokerException) {
                _state.update { if (it.status == null) it.copy(error = e.error) else it.copy(message = UiMessage.Failure(e.error)) }
            }
        }
        // Subtitles of «Приглашения» and «Пользователи»; best effort.
        viewModelScope.launch {
            try {
                val now = clock.now()
                val active = admin.invites().count { !it.isUsed && it.expiresAt?.isAfter(now) != false }
                _state.update { it.copy(activeInvites = active) }
            } catch (_: QuokerException) {
            }
        }
        viewModelScope.launch {
            try {
                val users = admin.users()
                _state.update { it.copy(users = UsersSummary(users.size, users.count { u -> u.status == UserStatus.BLOCKED })) }
            } catch (_: QuokerException) {
            }
        }
    }

    /** [uri]: what ACTION_OPEN_DOCUMENT returned, as a string. */
    fun onCookiesFilePicked(uri: String) {
        if (_state.value.uploadingCookies) return
        _state.update { it.copy(uploadingCookies = true, message = null) }
        viewModelScope.launch {
            try {
                when (val file = cookiesReader.read(uri)) {
                    CookiesFile.TooLarge -> fail(R.string.admin_cookies_too_large)
                    CookiesFile.Empty -> fail(R.string.admin_cookies_empty)
                    CookiesFile.Unreadable -> fail(R.string.admin_cookies_unreadable)
                    is CookiesFile.Content -> {
                        admin.uploadInstagramCookies(file.text)
                        val now = clock.now()
                        cookiesStore.markUploaded(now)
                        _state.update { it.copy(now = now, message = UiMessage.Info(R.string.admin_cookies_uploaded)) }
                    }
                }
            } catch (e: QuokerException) {
                _state.update { it.copy(message = UiMessage.Failure(e.error)) }
            } finally {
                _state.update { it.copy(uploadingCookies = false) }
            }
        }
    }

    fun dismissMessage() = _state.update { it.copy(message = null) }

    private fun fail(text: Int) = _state.update { it.copy(message = UiMessage.ErrorText(text)) }
}
