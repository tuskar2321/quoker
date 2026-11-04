package app.quoker.core.update

import app.quoker.core.common.QuokerError
import app.quoker.core.common.di.IoDispatcher
import app.quoker.core.data.model.AppVersion
import app.quoker.core.network.ApiUrls
import app.quoker.core.network.ErrorCodes
import app.quoker.core.network.QuokerException
import app.quoker.core.network.di.MediaHttpClient
import app.quoker.core.update.di.UpdatesDir
import java.io.File
import java.io.FileOutputStream
import java.io.IOException
import java.security.MessageDigest
import javax.inject.Inject
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.withContext
import okhttp3.OkHttpClient
import okhttp3.Request

/**
 * Downloads the release APK into [dir] with `Range` resume (`.part` file) and verifies its
 * sha256 against the version response. Throws [QuokerException] on failure.
 */
internal class ApkDownloader @Inject constructor(
    @MediaHttpClient private val client: OkHttpClient,
    private val urls: ApiUrls,
    @UpdatesDir private val dir: File,
    @IoDispatcher private val io: CoroutineDispatcher,
) {
    fun apkFile(release: AppVersion): File = File(dir, "quoker-${release.versionCode}.apk")

    private fun partFile(release: AppVersion): File = File(dir, "quoker-${release.versionCode}.apk.part")

    /** Removes every downloaded APK (nothing to install any more). */
    suspend fun clear() = withContext(io) {
        dir.listFiles()?.forEach { it.delete() }
    }

    suspend fun download(release: AppVersion, onProgress: (Int) -> Unit): File = withContext(io) {
        dir.mkdirs()
        val target = apkFile(release)
        val part = partFile(release)
        dir.listFiles()?.filter { it != target && it != part }?.forEach { it.delete() }

        if (target.exists()) {
            if (sha256(target).equals(release.sha256, ignoreCase = true)) return@withContext target
            target.delete()
        }

        fetch(release, part, onProgress, allowRestart = true)

        if (!sha256(part).equals(release.sha256, ignoreCase = true)) {
            part.delete()
            throw failure(UpdateErrorCodes.CHECKSUM_MISMATCH, "sha256 mismatch", retryable = true)
        }
        if (!part.renameTo(target)) {
            part.delete()
            throw failure(UpdateErrorCodes.DOWNLOAD_FAILED, "rename failed", retryable = true)
        }
        target
    }

    private suspend fun fetch(release: AppVersion, part: File, onProgress: (Int) -> Unit, allowRestart: Boolean) {
        val offset = if (part.exists()) part.length() else 0L
        val request = Request.Builder()
            .url(urls.absolute(release.apkUrl))
            .apply {
                if (offset > 0) {
                    header("Range", "bytes=$offset-")
                    // The core's ETag is the quoted sha256; a different file restarts with 200.
                    header("If-Range", "\"${release.sha256}\"")
                }
            }
            .build()

        val response = try {
            client.newCall(request).execute()
        } catch (e: IOException) {
            throw networkFailure(e)
        }
        response.use { resp ->
            when {
                resp.code == 416 && allowRestart -> {
                    part.delete()
                    return fetch(release, part, onProgress, allowRestart = false)
                }
                resp.code == 404 -> throw failure(UpdateErrorCodes.NO_RELEASE, "apk not found", retryable = true)
                !resp.isSuccessful -> throw failure(
                    UpdateErrorCodes.DOWNLOAD_FAILED,
                    "HTTP ${resp.code}",
                    retryable = true,
                )
            }
            val append = resp.code == 206 && offset > 0
            val start = if (append) offset else 0L
            val bodyLength = resp.body.contentLength()
            val total = if (bodyLength >= 0) start + bodyLength else -1L

            try {
                FileOutputStream(part, append).use { out ->
                    resp.body.byteStream().use { input ->
                        val buffer = ByteArray(BUFFER_SIZE)
                        var written = start
                        var lastPercent = -1
                        while (true) {
                            currentCoroutineContext().ensureActive()
                            val read = input.read(buffer)
                            if (read < 0) break
                            out.write(buffer, 0, read)
                            written += read
                            if (total > 0) {
                                val percent = (written * 100 / total).toInt().coerceIn(0, 100)
                                if (percent != lastPercent) {
                                    lastPercent = percent
                                    onProgress(percent)
                                }
                            }
                        }
                    }
                }
            } catch (e: IOException) {
                // The partial file stays for the next attempt's Range request.
                throw networkFailure(e)
            }
        }
    }

    private fun sha256(file: File): String {
        val digest = MessageDigest.getInstance("SHA-256")
        file.inputStream().use { input ->
            val buffer = ByteArray(BUFFER_SIZE)
            while (true) {
                val read = input.read(buffer)
                if (read < 0) break
                digest.update(buffer, 0, read)
            }
        }
        return digest.digest().joinToString("") { "%02x".format(it) }
    }

    private fun networkFailure(e: IOException) =
        e as? QuokerException ?: QuokerException(
            QuokerError(ErrorCodes.NETWORK, e.message ?: "network error", retryable = true),
            cause = e,
        )

    private fun failure(code: String, message: String, retryable: Boolean) =
        QuokerException(QuokerError(code, message, retryable))

    private companion object {
        const val BUFFER_SIZE = 64 * 1024
    }
}
