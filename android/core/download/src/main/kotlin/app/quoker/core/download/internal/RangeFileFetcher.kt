package app.quoker.core.download.internal

import app.quoker.core.common.QuokerError
import app.quoker.core.common.di.IoDispatcher
import app.quoker.core.network.ErrorCodes
import app.quoker.core.network.QuokerException
import app.quoker.core.network.di.MediaHttpClient
import app.quoker.core.network.model.ErrorBodyDto
import java.io.File
import java.io.FileOutputStream
import java.io.IOException
import javax.inject.Inject
import kotlin.coroutines.coroutineContext
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.Job
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.Json
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response

/**
 * Downloads a file into [File] with `Range` resume. The media client returns non-2xx responses, so
 * statuses are checked here; failures become [QuokerException] (409 file_not_ready and 429 are
 * retryable). A partial file is kept on transport errors so the next attempt resumes.
 */
class RangeFileFetcher @Inject constructor(
    @MediaHttpClient private val client: OkHttpClient,
    private val json: Json,
    @IoDispatcher private val io: CoroutineDispatcher,
) {
    suspend fun fetch(
        url: String,
        target: File,
        onProgress: suspend (downloaded: Long, total: Long?) -> Unit,
    ): Unit = withContext(io) {
        target.parentFile?.mkdirs()
        // At most one restart without Range (stale or mismatching partial file).
        repeat(2) {
            when (attempt(url, target, onProgress)) {
                Attempt.Done -> return@withContext
                Attempt.Restart -> target.delete()
            }
        }
        throw QuokerException(QuokerError(ErrorCodes.INTERNAL, "range resume failed", retryable = true))
    }

    private enum class Attempt { Done, Restart }

    private suspend fun attempt(
        url: String,
        target: File,
        onProgress: suspend (Long, Long?) -> Unit,
    ): Attempt {
        val existing = if (target.exists()) target.length() else 0L
        val request = Request.Builder().url(url)
            .apply { if (existing > 0) header("Range", "bytes=$existing-") }
            .build()
        val call = client.newCall(request)
        val cancelHandle = coroutineContext[Job]?.invokeOnCompletion { call.cancel() }
        try {
            val response = try {
                call.execute()
            } catch (e: QuokerException) {
                throw e
            } catch (e: IOException) {
                coroutineContext.ensureActive()
                throw network(e)
            }
            response.use { res ->
                return when (res.code) {
                    200 -> {
                        write(res, target, append = false, offset = 0, total = res.body.contentLength().takeIf { it >= 0 }, onProgress)
                        Attempt.Done
                    }
                    206 -> {
                        val range = ContentRange.parse(res.header("Content-Range"))
                        if (range == null || range.start != existing) return Attempt.Restart
                        write(res, target, append = true, offset = existing, total = range.total, onProgress)
                        Attempt.Done
                    }
                    416 -> {
                        val total = ContentRange.parse(res.header("Content-Range"))?.total
                        if (total != null && total == existing) {
                            onProgress(existing, total)
                            Attempt.Done
                        } else {
                            Attempt.Restart
                        }
                    }
                    else -> throw errorFor(res)
                }
            }
        } finally {
            cancelHandle?.dispose()
        }
    }

    private suspend fun write(
        res: Response,
        target: File,
        append: Boolean,
        offset: Long,
        total: Long?,
        onProgress: suspend (Long, Long?) -> Unit,
    ) {
        var downloaded = offset
        onProgress(downloaded, total)
        try {
            res.body.byteStream().use { input ->
                FileOutputStream(target, append).use { output ->
                    val buffer = ByteArray(BUFFER_SIZE)
                    while (true) {
                        coroutineContext.ensureActive()
                        val read = input.read(buffer)
                        if (read < 0) break
                        output.write(buffer, 0, read)
                        downloaded += read
                        onProgress(downloaded, total)
                    }
                }
            }
        } catch (e: IOException) {
            coroutineContext.ensureActive()
            throw network(e)
        }
        if (total != null && downloaded != total) {
            throw QuokerException(QuokerError(ErrorCodes.NETWORK, "short body: $downloaded of $total", retryable = true))
        }
    }

    private fun errorFor(res: Response): QuokerException {
        val body = try {
            res.peekBody(MAX_ERROR_BODY).string()
        } catch (_: IOException) {
            ""
        }
        val dto = runCatching { json.decodeFromString<ErrorBodyDto>(body).error }.getOrNull()
        val error = if (dto != null && dto.code.isNotEmpty()) {
            QuokerError(dto.code, dto.message, dto.retryable)
        } else {
            QuokerError(codeFor(res.code), "HTTP ${res.code}", retryable = res.code == 429 || res.code >= 500)
        }
        // file_not_ready means "prepare again"; too many parallel streams clears up by itself.
        val retryable = error.retryable || res.code == 409 || res.code == 429
        return QuokerException(
            error = error.copy(retryable = retryable),
            httpStatus = res.code,
            retryAfterSeconds = res.header("Retry-After")?.trim()?.toLongOrNull(),
        )
    }

    private fun codeFor(status: Int) = when (status) {
        401 -> ErrorCodes.UNAUTHORIZED
        403 -> ErrorCodes.FORBIDDEN
        404 -> ErrorCodes.NOT_FOUND
        409 -> ErrorCodes.FILE_NOT_READY
        426 -> ErrorCodes.UPGRADE_REQUIRED
        429 -> ErrorCodes.RATE_LIMITED
        502, 504 -> ErrorCodes.UPSTREAM_ERROR
        503 -> ErrorCodes.WORKER_UNAVAILABLE
        else -> ErrorCodes.INTERNAL
    }

    private fun network(e: IOException) =
        QuokerException(QuokerError(ErrorCodes.NETWORK, e.message ?: "network error", retryable = true), cause = e)

    private companion object {
        const val BUFFER_SIZE = 64 * 1024
        const val MAX_ERROR_BODY = 16L * 1024
    }
}

// Content-Range: "bytes 100-199/200" or "bytes */200".
internal data class ContentRange(val start: Long?, val total: Long?) {
    companion object {
        private val re = Regex("""^bytes\s+(?:(\d+)-\d+|\*)/(\d+|\*)$""")

        fun parse(header: String?): ContentRange? {
            val m = header?.trim()?.let(re::matchEntire) ?: return null
            return ContentRange(m.groupValues[1].toLongOrNull(), m.groupValues[2].toLongOrNull())
        }
    }
}
