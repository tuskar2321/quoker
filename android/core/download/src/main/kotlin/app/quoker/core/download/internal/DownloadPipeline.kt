package app.quoker.core.download.internal

import app.quoker.core.common.QuokerError
import app.quoker.core.data.model.PrepareResult
import app.quoker.core.data.model.TaskStatus
import app.quoker.core.data.repository.VideoRepository
import app.quoker.core.download.DownloadErrors
import app.quoker.core.network.ErrorCodes
import app.quoker.core.network.QuokerException
import java.io.File
import java.io.IOException
import javax.inject.Inject
import javax.inject.Qualifier
import kotlin.coroutines.cancellation.CancellationException
import kotlin.time.Duration
import kotlin.time.Duration.Companion.seconds
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.last

/** Directory for partial downloads (under cacheDir). */
@Qualifier
@Retention(AnnotationRetention.BINARY)
annotation class DownloadCacheDir

sealed interface DownloadProgress {
    data object Preparing : DownloadProgress

    data class Downloading(val percent: Int) : DownloadProgress
}

sealed interface DownloadOutcome {
    data class Saved(val uri: String) : DownloadOutcome

    data class Failed(val error: QuokerError) : DownloadOutcome
}

/**
 * prepare → poll the task until the file is ready → GET /file with Range into the cache →
 * MediaStore → POST /downloaded. Retryable failures (network, 409 file_not_ready, 429 too many
 * streams, 5xx) are retried with backoff, resuming the partial file.
 */
class DownloadPipeline @Inject constructor(
    private val videos: VideoRepository,
    private val fetcher: RangeFileFetcher,
    private val saver: VideoSaver,
    private val savedVideos: SavedVideos,
    @DownloadCacheDir private val cacheDir: File,
) {
    suspend fun run(videoId: String, onProgress: suspend (DownloadProgress) -> Unit): DownloadOutcome {
        val temp = tempFile(videoId)
        var attempt = 0
        var lastPercent = -1
        while (true) {
            try {
                onProgress(DownloadProgress.Preparing)
                awaitFileReady(videoId)
                fetcher.fetch(videos.fileUrl(videoId), temp) { done, total ->
                    val percent = if (total != null && total > 0) ((done * 100) / total).toInt().coerceIn(0, 100) else 0
                    if (percent != lastPercent) {
                        lastPercent = percent
                        onProgress(DownloadProgress.Downloading(percent))
                    }
                }
                break
            } catch (e: QuokerException) {
                attempt++
                if (!e.error.retryable || attempt >= MAX_ATTEMPTS) {
                    if (!e.error.retryable) temp.delete()
                    return DownloadOutcome.Failed(e.error)
                }
                lastPercent = -1
                delay(backoff(attempt, e.retryAfterSeconds))
            }
        }

        val uri = try {
            saver.save(temp, displayName(videoId))
        } catch (e: CancellationException) {
            throw e
        } catch (e: IOException) {
            return DownloadOutcome.Failed(DownloadErrors.storage(e.message ?: "storage error"))
        } catch (e: SecurityException) {
            return DownloadOutcome.Failed(DownloadErrors.storagePermission)
        }
        temp.delete()
        savedVideos.record(videoId, uri)
        markDownloaded(videoId)
        return DownloadOutcome.Saved(uri)
    }

    /** Removes the partial file, e.g. after the user cancelled. */
    fun discard(videoId: String) {
        tempFile(videoId).delete()
    }

    private suspend fun awaitFileReady(videoId: String) {
        when (val prepared = videos.prepare(videoId)) {
            is PrepareResult.Ready -> Unit
            is PrepareResult.Pending -> {
                val task = videos.pollTask(prepared.taskId).last()
                if (task.status == TaskStatus.FAILED) {
                    throw QuokerException(task.error ?: QuokerError(ErrorCodes.INTERNAL, "download task failed", retryable = false))
                }
            }
        }
    }

    // History is secondary to the saved file: a failure here does not fail the download.
    private suspend fun markDownloaded(videoId: String) {
        repeat(MARK_ATTEMPTS) { i ->
            try {
                videos.markDownloaded(videoId)
                return
            } catch (e: QuokerException) {
                if (!e.error.retryable) return
                delay(backoff(i + 1, null))
            }
        }
    }

    private fun tempFile(videoId: String) = File(cacheDir, "${safeName(videoId)}.part")

    companion object {
        const val MAX_ATTEMPTS = 6
        private const val MARK_ATTEMPTS = 3
        private val MAX_BACKOFF = 30.seconds

        fun displayName(videoId: String) = "quoker_${safeName(videoId)}.mp4"

        private fun safeName(videoId: String) = videoId.replace(Regex("[^A-Za-z0-9_-]"), "_")

        internal fun backoff(attempt: Int, retryAfterSeconds: Long?): Duration {
            retryAfterSeconds?.let { return it.coerceIn(1, 120).seconds }
            val seconds = 1L shl (attempt.coerceIn(1, 6))
            return seconds.seconds.coerceAtMost(MAX_BACKOFF)
        }
    }
}
