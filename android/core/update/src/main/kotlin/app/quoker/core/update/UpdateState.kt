package app.quoker.core.update

import app.quoker.core.common.QuokerError
import app.quoker.core.data.model.AppVersion

/** What the UI shows about a newer release. */
data class UpdateInfo(
    val versionCode: Int,
    val versionName: String,
    val changelog: String,
)

internal fun AppVersion.toInfo() = UpdateInfo(versionCode, versionName, changelog)

sealed interface UpdateState {
    /** Null only for a blocking state reached through a 426 before the version is fetched. */
    val info: UpdateInfo? get() = null

    /** True when the app is below min_supported_version_code: show [UpgradeRequiredScreen]. */
    val required: Boolean get() = false

    data object None : UpdateState

    data class Available(override val info: UpdateInfo) : UpdateState

    data class Required(override val info: UpdateInfo?) : UpdateState {
        override val required: Boolean get() = true
    }

    data class Downloading(
        val percent: Int,
        override val info: UpdateInfo?,
        override val required: Boolean,
    ) : UpdateState

    data class ReadyToInstall(
        override val info: UpdateInfo?,
        override val required: Boolean,
    ) : UpdateState

    data class Failed(
        val error: QuokerError,
        override val info: UpdateInfo?,
        override val required: Boolean,
    ) : UpdateState
}

/** Result of [UpdateManager.checkNow] for the Settings «Проверить обновления» button. */
sealed interface UpdateCheckResult {
    /** «У вас последняя версия». */
    data object UpToDate : UpdateCheckResult

    /** A newer release exists; [UpdateManager.state] now shows it. */
    data class Found(val info: UpdateInfo, val required: Boolean) : UpdateCheckResult

    data class Failed(val error: QuokerError) : UpdateCheckResult
}

/** Client-only error codes of this module; texts in this module's strings.xml. */
object UpdateErrorCodes {
    const val CHECKSUM_MISMATCH = "update_checksum_mismatch"
    const val DOWNLOAD_FAILED = "update_download_failed"
    const val INSTALL_FAILED = "update_install_failed"
    const val NO_RELEASE = "update_no_release"
}
