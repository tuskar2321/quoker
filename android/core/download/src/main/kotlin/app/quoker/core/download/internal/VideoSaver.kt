package app.quoker.core.download.internal

import android.content.ContentResolver
import android.content.ContentUris
import android.content.ContentValues
import android.content.Context
import android.net.Uri
import android.os.Build
import android.os.Environment
import android.provider.MediaStore
import androidx.annotation.RequiresApi
import app.quoker.core.common.di.IoDispatcher
import dagger.hilt.android.qualifiers.ApplicationContext
import java.io.File
import java.io.IOException
import javax.inject.Inject
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.withContext

/** Publishes a downloaded file to the gallery; returns the `content://` Uri. */
interface VideoSaver {
    /** @throws IOException when the file cannot be written. */
    suspend fun save(source: File, displayName: String): String
}

/**
 * MediaStore `Movies/Quoker`: API 29+ inserts with IS_PENDING and streams the file in; API 26–28
 * copies into the public directory (WRITE_EXTERNAL_STORAGE) and registers the path.
 */
class MediaStoreVideoSaver @Inject constructor(
    @ApplicationContext private val context: Context,
    @IoDispatcher private val io: CoroutineDispatcher,
) : VideoSaver {
    override suspend fun save(source: File, displayName: String): String = withContext(io) {
        val resolver = context.contentResolver
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) saveScoped(resolver, source, displayName)
        else saveLegacy(resolver, source, displayName)
    }

    @RequiresApi(Build.VERSION_CODES.Q)
    private fun saveScoped(resolver: ContentResolver, source: File, displayName: String): String {
        val collection = MediaStore.Video.Media.getContentUri(MediaStore.VOLUME_EXTERNAL_PRIMARY)
        val values = ContentValues().apply {
            put(MediaStore.MediaColumns.DISPLAY_NAME, displayName)
            put(MediaStore.MediaColumns.MIME_TYPE, MIME_TYPE)
            put(MediaStore.MediaColumns.RELATIVE_PATH, "${Environment.DIRECTORY_MOVIES}/$ALBUM")
            put(MediaStore.MediaColumns.IS_PENDING, 1)
        }
        val uri = resolver.insert(collection, values) ?: throw IOException("MediaStore insert returned null")
        try {
            val out = resolver.openOutputStream(uri) ?: throw IOException("no output stream for $uri")
            out.use { o -> source.inputStream().use { it.copyTo(o) } }
            val publish = ContentValues().apply { put(MediaStore.MediaColumns.IS_PENDING, 0) }
            resolver.update(uri, publish, null, null)
        } catch (e: Exception) {
            runCatching { resolver.delete(uri, null, null) }
            throw e as? IOException ?: IOException(e)
        }
        return uri.toString()
    }

    @Suppress("DEPRECATION") // DATA and the public directory are the only way below API 29.
    private fun saveLegacy(resolver: ContentResolver, source: File, displayName: String): String {
        val dir = File(Environment.getExternalStoragePublicDirectory(Environment.DIRECTORY_MOVIES), ALBUM)
        if (!dir.exists() && !dir.mkdirs()) throw IOException("cannot create $dir")
        val target = uniqueFile(dir, displayName)
        try {
            source.copyTo(target)
        } catch (e: Exception) {
            target.delete()
            throw e as? IOException ?: IOException(e)
        }
        val values = ContentValues().apply {
            put(MediaStore.MediaColumns.DATA, target.absolutePath)
            put(MediaStore.MediaColumns.DISPLAY_NAME, target.name)
            put(MediaStore.MediaColumns.MIME_TYPE, MIME_TYPE)
        }
        val uri: Uri = resolver.insert(MediaStore.Video.Media.EXTERNAL_CONTENT_URI, values)
            ?: findByPath(resolver, target)
            ?: throw IOException("MediaStore insert returned null").also { target.delete() }
        return uri.toString()
    }

    @Suppress("DEPRECATION")
    private fun findByPath(resolver: ContentResolver, file: File): Uri? {
        val collection = MediaStore.Video.Media.EXTERNAL_CONTENT_URI
        resolver.query(
            collection,
            arrayOf(MediaStore.MediaColumns._ID),
            "${MediaStore.MediaColumns.DATA} = ?",
            arrayOf(file.absolutePath),
            null,
        )?.use { c -> if (c.moveToFirst()) return ContentUris.withAppendedId(collection, c.getLong(0)) }
        return null
    }

    private fun uniqueFile(dir: File, name: String): File {
        val base = name.substringBeforeLast('.')
        val ext = name.substringAfterLast('.', "")
        var candidate = File(dir, name)
        var n = 1
        while (candidate.exists()) {
            candidate = File(dir, if (ext.isEmpty()) "$base ($n)" else "$base ($n).$ext")
            n++
        }
        return candidate
    }

    companion object {
        const val ALBUM = "Quoker"
        const val MIME_TYPE = "video/mp4"
    }
}
