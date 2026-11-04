package app.quoker.core.download

import android.content.Context
import android.os.Build
import androidx.hilt.work.HiltWorker
import androidx.work.CoroutineWorker
import androidx.work.Data
import androidx.work.ForegroundInfo
import androidx.work.WorkInfo
import androidx.work.WorkerParameters
import androidx.work.workDataOf
import app.quoker.core.common.QuokerError
import app.quoker.core.download.internal.DownloadNotifier
import app.quoker.core.download.internal.DownloadOutcome
import app.quoker.core.download.internal.DownloadPipeline
import app.quoker.core.download.internal.DownloadProgress
import app.quoker.core.network.ErrorCodes
import dagger.assisted.Assisted
import dagger.assisted.AssistedInject
import kotlin.coroutines.cancellation.CancellationException

/**
 * Expedited foreground (dataSync) work that saves one video to the gallery. Input: [KEY_VIDEO_ID],
 * [KEY_TITLE]. Progress: [KEY_PHASE] + [KEY_PERCENT]. Output: [KEY_URI] on success, the
 * QuokerError fields on failure.
 */
@HiltWorker
class DownloadWorker @AssistedInject constructor(
    @Assisted context: Context,
    @Assisted params: WorkerParameters,
    private val pipeline: DownloadPipeline,
    private val notifier: DownloadNotifier,
) : CoroutineWorker(context, params) {

    private val title: String? get() = inputData.getString(KEY_TITLE)

    override suspend fun getForegroundInfo(): ForegroundInfo = notifier.progressForeground(id, title, null)

    override suspend fun doWork(): Result {
        val videoId = inputData.getString(KEY_VIDEO_ID)
            ?: return Result.failure(errorData(QuokerError(ErrorCodes.INVALID_REQUEST, "no video id", false)))
        updateForeground(null)
        val outcome = try {
            pipeline.run(videoId) { progress ->
                setProgress(progress.toData())
                updateForeground((progress as? DownloadProgress.Downloading)?.percent)
            }
        } catch (e: CancellationException) {
            // VideoDownloads.cancel already discards the partial file; on API 31+ the stop reason
            // also catches a write that raced with it (stopReason does not exist before 31).
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S && stopReason == WorkInfo.STOP_REASON_CANCELLED_BY_APP) {
                pipeline.discard(videoId)
            }
            throw e
        }
        return when (outcome) {
            is DownloadOutcome.Saved -> {
                notifier.saved(videoId, title, outcome.uri)
                Result.success(workDataOf(KEY_URI to outcome.uri))
            }
            is DownloadOutcome.Failed -> {
                notifier.failed(videoId, title, outcome.error)
                Result.failure(errorData(outcome.error))
            }
        }
    }

    // Can fail when the app may not start a foreground service now; the work still runs.
    private suspend fun updateForeground(percent: Int?) {
        try {
            setForeground(notifier.progressForeground(id, title, percent))
        } catch (e: CancellationException) {
            throw e
        } catch (_: IllegalStateException) {
        }
    }

    companion object {
        const val KEY_VIDEO_ID = "video_id"
        const val KEY_TITLE = "title"
        const val KEY_PHASE = "phase"
        const val KEY_PERCENT = "percent"
        const val KEY_URI = "uri"
        const val KEY_ERROR_CODE = "error_code"
        const val KEY_ERROR_MESSAGE = "error_message"
        const val KEY_ERROR_RETRYABLE = "error_retryable"
        const val PHASE_PREPARING = "preparing"
        const val PHASE_DOWNLOADING = "downloading"

        internal fun DownloadProgress.toData(): Data = when (this) {
            DownloadProgress.Preparing -> workDataOf(KEY_PHASE to PHASE_PREPARING)
            is DownloadProgress.Downloading -> workDataOf(KEY_PHASE to PHASE_DOWNLOADING, KEY_PERCENT to percent)
        }

        internal fun errorData(error: QuokerError): Data = workDataOf(
            KEY_ERROR_CODE to error.code,
            KEY_ERROR_MESSAGE to error.message,
            KEY_ERROR_RETRYABLE to error.retryable,
        )

        internal fun Data.toError(): QuokerError = QuokerError(
            code = getString(KEY_ERROR_CODE) ?: ErrorCodes.INTERNAL,
            message = getString(KEY_ERROR_MESSAGE).orEmpty(),
            retryable = getBoolean(KEY_ERROR_RETRYABLE, true),
        )
    }
}
