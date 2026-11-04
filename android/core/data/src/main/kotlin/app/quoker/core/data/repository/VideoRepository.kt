package app.quoker.core.data.repository

import app.quoker.core.data.model.PrepareResult
import app.quoker.core.data.model.ResolveResult
import app.quoker.core.data.model.Task
import app.quoker.core.data.model.Video
import app.quoker.core.data.model.toDomain
import app.quoker.core.network.ApiUrls
import app.quoker.core.network.ErrorCodes
import app.quoker.core.network.QuokerApi
import app.quoker.core.network.QuokerException
import app.quoker.core.network.model.ResolveRequestDto
import app.quoker.core.network.quokerCall
import app.quoker.core.common.QuokerError
import javax.inject.Inject
import kotlin.time.Duration
import kotlin.time.Duration.Companion.seconds
import kotlinx.coroutines.flow.Flow

interface VideoRepository {
    /** POST /v1/videos/resolve: the video, or a task to [pollTask] when the core needs longer. */
    suspend fun resolve(url: String): ResolveResult

    suspend fun task(taskId: String): Task

    /** Emits task states until DONE (with video) or FAILED (with error). */
    fun pollTask(taskId: String, interval: Duration = TASK_POLL_INTERVAL): Flow<Task>

    suspend fun get(videoId: String): Video

    /** POST /prepare: READY with size, or a download task to poll. */
    suspend fun prepare(videoId: String): PrepareResult

    /** Records the download in history after the file is saved. */
    suspend fun markDownloaded(videoId: String)

    /** Absolute thumbnail URL; load it with the `@MediaHttpClient` OkHttp client. */
    fun thumbnailUrl(videoId: String): String

    /** Absolute `GET /file` URL (supports Range); use the `@MediaHttpClient` OkHttp client. */
    fun fileUrl(videoId: String): String

    companion object {
        val TASK_POLL_INTERVAL: Duration = 1.seconds
    }
}

internal class VideoRepositoryImpl @Inject constructor(
    private val api: QuokerApi,
    private val urls: ApiUrls,
) : VideoRepository {
    override suspend fun resolve(url: String): ResolveResult {
        val res = quokerCall { api.resolve(ResolveRequestDto(url)) }
        return when {
            res.video != null -> ResolveResult.Ready(res.video!!.toDomain(urls))
            res.taskId != null -> ResolveResult.Pending(res.taskId!!)
            else -> throw malformed("resolve response has neither video nor task_id")
        }
    }

    override suspend fun task(taskId: String): Task = quokerCall { api.task(taskId) }.toDomain(urls)

    override fun pollTask(taskId: String, interval: Duration): Flow<Task> =
        poll(interval, { api.task(taskId).toDomain(urls) }) { it.status.isTerminal }

    override suspend fun get(videoId: String): Video = quokerCall { api.video(videoId) }.video.toDomain(urls)

    override suspend fun prepare(videoId: String): PrepareResult {
        val res = quokerCall { api.prepare(videoId) }
        return when {
            res.taskId != null -> PrepareResult.Pending(res.taskId!!)
            res.file != null -> PrepareResult.Ready(res.file!!.sizeBytes)
            else -> throw malformed("prepare response has neither file nor task_id")
        }
    }

    override suspend fun markDownloaded(videoId: String) = quokerCall { api.markDownloaded(videoId) }

    override fun thumbnailUrl(videoId: String): String = urls.thumbnail(videoId)

    override fun fileUrl(videoId: String): String = urls.file(videoId)

    private fun malformed(message: String) =
        QuokerException(QuokerError(ErrorCodes.INTERNAL, message, retryable = false))
}
