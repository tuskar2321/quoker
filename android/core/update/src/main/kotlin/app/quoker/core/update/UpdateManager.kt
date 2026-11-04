package app.quoker.core.update

import android.app.Activity
import android.content.pm.PackageInstaller
import app.quoker.core.common.QuokerError
import app.quoker.core.data.di.ApplicationScope
import app.quoker.core.data.model.AppVersion
import app.quoker.core.data.model.AppVersionCheck
import app.quoker.core.data.repository.AppVersionRepository
import app.quoker.core.network.QuokerException
import app.quoker.core.network.SessionEvent
import app.quoker.core.network.SessionEvents
import app.quoker.core.update.install.ApkInstaller
import java.io.File
import java.util.concurrent.atomic.AtomicBoolean
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.filterIsInstance
import kotlinx.coroutines.flow.launchIn
import kotlinx.coroutines.flow.onEach
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

/**
 * App update flow (F-24/F-25): check → dialog or blocking screen → APK download → installer.
 * Also turns any 426 from the API ([SessionEvent.UpgradeRequired]) into [UpdateState.Required].
 */
@Singleton
class UpdateManager @Inject internal constructor(
    private val repository: AppVersionRepository,
    private val downloader: ApkDownloader,
    private val installer: ApkInstaller,
    sessionEvents: SessionEvents,
    @ApplicationScope private val scope: CoroutineScope,
) {
    private val _state = MutableStateFlow<UpdateState>(UpdateState.None)
    val state: StateFlow<UpdateState> = _state.asStateFlow()

    private val startChecked = AtomicBoolean(false)
    private var release: AppVersion? = null
    private var downloadJob: Job? = null
    private var apk: File? = null

    init {
        sessionEvents.events
            .filterIsInstance<SessionEvent.UpgradeRequired>()
            .onEach { onUpgradeRequired() }
            .launchIn(scope)
    }

    /** At app start: GET /v1/app/version at most once a day (and once per process). Errors are silent. */
    fun checkOnStart() {
        if (!startChecked.compareAndSet(false, true)) return
        scope.launch {
            val check = try {
                repository.checkDaily()
            } catch (e: QuokerException) {
                return@launch
            }
            if (check != null) apply(check)
        }
    }

    /** «Проверить обновления» in Settings: ignores the daily limit. */
    suspend fun checkNow(): UpdateCheckResult {
        val check = try {
            repository.check()
        } catch (e: QuokerException) {
            return UpdateCheckResult.Failed(e.error)
        }
        if (check == null) return UpdateCheckResult.UpToDate
        apply(check)
        return if (check.updateAvailable || check.upgradeRequired) {
            UpdateCheckResult.Found(check.latest.toInfo(), check.upgradeRequired)
        } else {
            UpdateCheckResult.UpToDate
        }
    }

    /** The single «Обновить» action: downloads, or launches the installer once downloaded. */
    fun proceed(activity: Activity) {
        if (_state.value is UpdateState.ReadyToInstall) install(activity) else download()
    }

    /** «Позже». Ignored while the update is required; a running download is cancelled (resumable). */
    fun dismiss() {
        if (_state.value.required) return
        downloadJob?.cancel()
        downloadJob = null
        _state.value = UpdateState.None
    }

    fun download() {
        if (downloadJob?.isActive == true) return
        val current = _state.value
        if (current is UpdateState.None) return
        val required = current.required
        downloadJob = scope.launch {
            try {
                val target = release ?: fetchReleaseFor(required) ?: return@launch
                val info = target.toInfo()
                _state.value = UpdateState.Downloading(0, info, required)
                val file = downloader.download(target) { percent ->
                    _state.update { s -> if (s is UpdateState.Downloading) s.copy(percent = percent) else s }
                }
                apk = file
                _state.value = UpdateState.ReadyToInstall(info, required)
            } catch (e: CancellationException) {
                throw e
            } catch (e: QuokerException) {
                val info = release?.toInfo()
                // The release was replaced on the core: the retry fetches the current one.
                if (e.code == UpdateErrorCodes.NO_RELEASE) release = null
                _state.value = UpdateState.Failed(e.error, info, required)
            }
        }
    }

    /**
     * Launches the system installer for the downloaded APK. Without «install unknown apps»
     * permission opens its settings screen instead; call again when the user returns.
     */
    fun install(activity: Activity) {
        val current = _state.value as? UpdateState.ReadyToInstall ?: return
        val file = apk ?: return
        if (!installer.canInstall()) {
            installer.requestPermission(activity)
            return
        }
        scope.launch {
            try {
                installer.install(file)
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                _state.value = UpdateState.Failed(installError(e.message), current.info, current.required)
            }
        }
    }

    internal fun onInstallStatus(status: Int, message: String?) {
        val current = _state.value
        when (status) {
            PackageInstaller.STATUS_SUCCESS -> Unit // The process is replaced by the new version.
            PackageInstaller.STATUS_FAILURE_ABORTED -> Unit // User cancelled; «Установить» stays.
            else -> _state.value = UpdateState.Failed(installError(message), current.info, current.required)
        }
    }

    /** 426 from any API call; also exposed for callers that see it outside [SessionEvents]. */
    fun onUpgradeRequired() {
        if (_state.value.required) return
        _state.value = UpdateState.Required(release?.toInfo())
        scope.launch {
            val check = try {
                repository.check()
            } catch (e: QuokerException) {
                return@launch
            }
            if (check != null) {
                release = check.latest
                _state.update { s -> if (s is UpdateState.Required) UpdateState.Required(check.latest.toInfo()) else s }
            }
        }
    }

    private suspend fun apply(check: AppVersionCheck) {
        val current = _state.value
        if (current is UpdateState.Downloading || current is UpdateState.ReadyToInstall) return
        release = check.latest
        val info = check.latest.toInfo()
        _state.value = when {
            check.upgradeRequired -> UpdateState.Required(info)
            check.updateAvailable -> UpdateState.Available(info)
            current.required -> current // A 426 is authoritative until the core says otherwise.
            else -> UpdateState.None
        }
        if (!check.updateAvailable && !check.upgradeRequired) downloader.clear()
    }

    /** Release unknown (426 before the version request succeeded): fetch it now. */
    private suspend fun fetchReleaseFor(required: Boolean): AppVersion? {
        val check = repository.check()
        if (check == null) {
            _state.value = UpdateState.Failed(
                QuokerError(UpdateErrorCodes.NO_RELEASE, "no release published", retryable = true),
                null,
                required,
            )
            return null
        }
        release = check.latest
        return check.latest
    }

    private fun installError(message: String?) =
        QuokerError(UpdateErrorCodes.INSTALL_FAILED, message ?: "install failed", retryable = true)
}
