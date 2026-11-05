package app.quoker.core.download.internal

import android.content.ContentProvider
import android.content.ContentUris
import android.content.ContentValues
import android.database.Cursor
import android.database.MatrixCursor
import android.net.Uri
import android.provider.MediaStore
import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import java.io.File
import java.nio.file.Files
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.runTest
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric

/** MediaStore with a fixed set of row ids; any other id has no row. */
class RowsProvider : ContentProvider() {
    val ids = mutableSetOf<Long>()

    override fun onCreate() = true

    override fun query(uri: Uri, p: Array<out String>?, s: String?, a: Array<out String>?, o: String?): Cursor =
        MatrixCursor(arrayOf(MediaStore.MediaColumns._ID)).apply {
            val id = ContentUris.parseId(uri)
            if (id in ids) addRow(arrayOf(id))
        }

    override fun insert(uri: Uri, values: ContentValues?): Uri? = null

    override fun update(uri: Uri, values: ContentValues?, selection: String?, args: Array<out String>?) = 0

    override fun delete(uri: Uri, selection: String?, args: Array<out String>?) = 0

    override fun getType(uri: Uri): String = "video/mp4"
}

@RunWith(AndroidJUnit4::class)
class MediaStoreSavedVideosTest {
    private val context get() = ApplicationProvider.getApplicationContext<android.content.Context>()
    private val dir: File = Files.createTempDirectory("saved").toFile()
    private lateinit var provider: RowsProvider

    @Before
    fun setUp() {
        provider = Robolectric.setupContentProvider(RowsProvider::class.java, MediaStore.AUTHORITY)
    }

    @After
    fun tearDown() {
        dir.deleteRecursively()
    }

    private fun TestScope.saved(): MediaStoreSavedVideos {
        val store = PreferenceDataStoreFactory.create(scope = TestScope(UnconfinedTestDispatcher(testScheduler))) {
            File(dir, "saved.preferences_pb")
        }
        return MediaStoreSavedVideos(context, store, Dispatchers.Unconfined)
    }

    @Test
    fun recordsUriPerVideo() = runTest {
        val saved = saved()
        assertNull(saved.observe("tiktok:1").first())
        saved.record("tiktok:1", "content://media/external/video/media/5")
        saved.record("tiktok:1", "content://media/external/video/media/6")
        assertEquals("content://media/external/video/media/6", saved.observe("tiktok:1").first())
        assertNull(saved.observe("tiktok:2").first())
    }

    @Test
    fun existsChecksTheMediaStoreRow() = runTest {
        provider.ids += 5
        val saved = saved()
        val cases = listOf(
            "content://media/external/video/media/5" to true,
            "content://media/external/video/media/6" to false,
            "content://app.quoker.unknown/1" to false,
        )
        for ((uri, expected) in cases) {
            if (expected) assertTrue(uri, saved.exists(uri)) else assertFalse(uri, saved.exists(uri))
        }
    }
}
