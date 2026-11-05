package app.quoker.core.download.internal

import android.content.Context
import android.provider.MediaStore
import androidx.core.net.toUri
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.edit
import androidx.datastore.preferences.core.stringPreferencesKey
import app.quoker.core.common.di.IoDispatcher
import dagger.hilt.android.qualifiers.ApplicationContext
import javax.inject.Inject
import javax.inject.Qualifier
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.withContext

@Qualifier
@Retention(AnnotationRetention.BINARY)
annotation class SavedVideosPreferences

/**
 * Where each video was saved in the gallery. Outlives WorkManager's finished work (which it prunes),
 * so «Сохранено · Открыть» survives; the file itself is checked before it is shown.
 */
interface SavedVideos {
    suspend fun record(videoId: String, uri: String)

    /** The last saved `content://` uri of [videoId], or null. */
    fun observe(videoId: String): Flow<String?>

    /** The MediaStore row still exists (the user did not delete the video). */
    suspend fun exists(uri: String): Boolean
}

class MediaStoreSavedVideos @Inject constructor(
    @ApplicationContext private val context: Context,
    @SavedVideosPreferences private val store: DataStore<Preferences>,
    @IoDispatcher private val io: CoroutineDispatcher,
) : SavedVideos {
    override suspend fun record(videoId: String, uri: String) {
        store.edit { it[key(videoId)] = uri }
    }

    override fun observe(videoId: String): Flow<String?> =
        store.data.map { it[key(videoId)] }.distinctUntilChanged()

    override suspend fun exists(uri: String): Boolean = withContext(io) {
        try {
            context.contentResolver
                .query(uri.toUri(), arrayOf(MediaStore.MediaColumns._ID), null, null, null)
                ?.use { it.moveToFirst() }
                ?: false
        } catch (_: SecurityException) {
            false // Not ours any more (e.g. restored from a backup after reinstall).
        } catch (_: IllegalArgumentException) {
            false
        }
    }

    private fun key(videoId: String) = stringPreferencesKey("uri:$videoId")
}
