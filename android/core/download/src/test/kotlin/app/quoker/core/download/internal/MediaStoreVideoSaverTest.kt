package app.quoker.core.download.internal

import android.content.ContentProvider
import android.content.ContentUris
import android.content.ContentValues
import android.database.Cursor
import android.database.MatrixCursor
import android.net.Uri
import android.os.ParcelFileDescriptor
import android.provider.MediaStore
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import java.io.File
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.annotation.Config

/** Minimal in-memory MediaStore: rows with values, files backed by temp files. */
class FakeMediaProvider : ContentProvider() {
    val rows = mutableMapOf<Long, ContentValues>()
    val files = mutableMapOf<Long, File>()
    private var nextId = 1L

    override fun onCreate() = true

    override fun insert(uri: Uri, values: ContentValues?): Uri {
        val id = nextId++
        rows[id] = ContentValues(values)
        return ContentUris.withAppendedId(uri, id)
    }

    override fun update(uri: Uri, values: ContentValues?, selection: String?, args: Array<out String>?): Int {
        rows[ContentUris.parseId(uri)]?.putAll(values) ?: return 0
        return 1
    }

    override fun delete(uri: Uri, selection: String?, args: Array<out String>?): Int =
        if (rows.remove(ContentUris.parseId(uri)) != null) 1 else 0

    override fun openFile(uri: Uri, mode: String): ParcelFileDescriptor {
        val file = files.getOrPut(ContentUris.parseId(uri)) { File.createTempFile("media", ".mp4") }
        return ParcelFileDescriptor.open(file, ParcelFileDescriptor.parseMode(mode))
    }

    override fun query(uri: Uri, p: Array<out String>?, s: String?, a: Array<out String>?, o: String?): Cursor =
        MatrixCursor(arrayOf(MediaStore.MediaColumns._ID))

    override fun getType(uri: Uri): String = "video/mp4"
}

@RunWith(AndroidJUnit4::class)
class MediaStoreVideoSaverTest {
    private lateinit var provider: FakeMediaProvider
    private val context get() = ApplicationProvider.getApplicationContext<android.content.Context>()
    private val bytes = ByteArray(2048) { (it % 7).toByte() }

    @Before
    fun setUp() {
        provider = Robolectric.setupContentProvider(FakeMediaProvider::class.java, MediaStore.AUTHORITY)
    }

    private fun source() = File(context.cacheDir, "v.part").apply { writeBytes(bytes) }

    @Test
    @Config(sdk = [34])
    fun scopedStorageUsesPendingRow() = runTest {
        val uri = MediaStoreVideoSaver(context, Dispatchers.Unconfined).save(source(), "quoker_tiktok_1.mp4")
        val id = ContentUris.parseId(Uri.parse(uri))
        val row = provider.rows.getValue(id)
        assertEquals("Movies/Quoker", row.getAsString(MediaStore.MediaColumns.RELATIVE_PATH))
        assertEquals("quoker_tiktok_1.mp4", row.getAsString(MediaStore.MediaColumns.DISPLAY_NAME))
        assertEquals(0, row.getAsInteger(MediaStore.MediaColumns.IS_PENDING))
        assertArrayEquals(bytes, provider.files.getValue(id).readBytes())
    }

    @Test
    @Config(sdk = [28])
    @Suppress("DEPRECATION")
    fun legacyStorageCopiesIntoMoviesQuoker() = runTest {
        val saver = MediaStoreVideoSaver(context, Dispatchers.Unconfined)
        val first = saver.save(source(), "quoker_tiktok_1.mp4")
        val second = saver.save(source(), "quoker_tiktok_1.mp4")
        val paths = provider.rows.values.map { it.getAsString(MediaStore.MediaColumns.DATA) }
        assertTrue(paths[0].endsWith("Movies/Quoker/quoker_tiktok_1.mp4"))
        assertTrue(paths[1].endsWith("Movies/Quoker/quoker_tiktok_1 (1).mp4"))
        assertArrayEquals(bytes, File(paths[0]).readBytes())
        assertTrue(first != second)
    }
}
