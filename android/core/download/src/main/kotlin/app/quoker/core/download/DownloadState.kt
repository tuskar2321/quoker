package app.quoker.core.download

import android.content.res.Resources
import app.quoker.core.common.QuokerError
import app.quoker.core.designsystem.text.userMessage

/** State of the phone download of one video, as the "Видео" sheet shows it. */
sealed interface DownloadState {
    data object Idle : DownloadState

    /** Waiting for the network or for the core to prepare the file. */
    data object Preparing : DownloadState

    data class Downloading(val percent: Int) : DownloadState

    /** [uri]: `content://` Uri of the saved video in MediaStore. */
    data class Saved(val uri: String) : DownloadState

    data class Failed(val error: QuokerError) : DownloadState
}

/** Client-only error codes of the download (in addition to docs/03 codes). */
object DownloadErrors {
    /** Copying into MediaStore failed (no space, storage unavailable). */
    const val STORAGE = "storage_failed"

    /** API 26–28: WRITE_EXTERNAL_STORAGE was denied. */
    const val STORAGE_PERMISSION = "storage_permission"

    fun storage(message: String) = QuokerError(STORAGE, message, retryable = true)

    val storagePermission = QuokerError(STORAGE_PERMISSION, "storage permission denied", retryable = true)
}

/** UI text for a download error: client codes first, then `error_<code>`, then the server message. */
fun QuokerError.downloadMessage(resources: Resources): String = when (code) {
    DownloadErrors.STORAGE -> resources.getString(R.string.download_error_storage)
    DownloadErrors.STORAGE_PERMISSION -> resources.getString(R.string.download_error_storage_permission)
    else -> userMessage(resources)
}
