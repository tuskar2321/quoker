package app.quoker.feature.settings.data

import android.content.Context
import android.net.Uri
import app.quoker.core.common.di.IoDispatcher
import dagger.hilt.android.qualifiers.ApplicationContext
import java.io.IOException
import javax.inject.Inject
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.withContext

sealed interface CookiesFile {
    data class Content(val text: String) : CookiesFile
    data object TooLarge : CookiesFile
    data object Empty : CookiesFile
    data object Unreadable : CookiesFile
}

/** Reads a file picked through SAF; [uri] is the `content://` URI as a string. */
fun interface CookiesFileReader {
    suspend fun read(uri: String): CookiesFile

    companion object {
        /** docs/11: cookies up to 64 KB. */
        const val MAX_BYTES = 64 * 1024
    }
}

internal class ContentResolverCookiesFileReader @Inject constructor(
    @ApplicationContext private val context: Context,
    @IoDispatcher private val io: CoroutineDispatcher,
) : CookiesFileReader {
    override suspend fun read(uri: String): CookiesFile = withContext(io) {
        try {
            val stream = context.contentResolver.openInputStream(Uri.parse(uri)) ?: return@withContext CookiesFile.Unreadable
            // One byte over the limit is enough to tell that the file is too large.
            val bytes = stream.use { it.readNBytesCompat(CookiesFileReader.MAX_BYTES + 1) }
            when {
                bytes.size > CookiesFileReader.MAX_BYTES -> CookiesFile.TooLarge
                else -> {
                    val text = bytes.toString(Charsets.UTF_8)
                    if (text.isBlank()) CookiesFile.Empty else CookiesFile.Content(text)
                }
            }
        } catch (_: IOException) {
            CookiesFile.Unreadable
        } catch (_: SecurityException) {
            CookiesFile.Unreadable
        }
    }
}

private fun java.io.InputStream.readNBytesCompat(limit: Int): ByteArray {
    val buffer = ByteArray(limit)
    var total = 0
    while (total < limit) {
        val n = read(buffer, total, limit - total)
        if (n < 0) break
        total += n
    }
    return buffer.copyOf(total)
}
