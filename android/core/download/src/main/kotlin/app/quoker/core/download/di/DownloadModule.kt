package app.quoker.core.download.di

import android.content.Context
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.preferencesDataStoreFile
import app.quoker.core.data.di.ApplicationScope
import app.quoker.core.download.VideoDownloads
import app.quoker.core.download.WorkManagerVideoDownloads
import app.quoker.core.download.internal.DownloadCacheDir
import app.quoker.core.download.internal.MediaStoreSavedVideos
import app.quoker.core.download.internal.MediaStoreVideoSaver
import app.quoker.core.download.internal.SavedVideos
import app.quoker.core.download.internal.SavedVideosPreferences
import app.quoker.core.download.internal.VideoSaver
import dagger.Binds
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.components.SingletonComponent
import java.io.File
import javax.inject.Singleton
import kotlinx.coroutines.CoroutineScope

@Module
@InstallIn(SingletonComponent::class)
internal abstract class DownloadModule {
    @Binds
    abstract fun bindVideoDownloads(impl: WorkManagerVideoDownloads): VideoDownloads

    @Binds
    abstract fun bindVideoSaver(impl: MediaStoreVideoSaver): VideoSaver

    @Binds
    abstract fun bindSavedVideos(impl: MediaStoreSavedVideos): SavedVideos

    companion object {
        @Provides
        @DownloadCacheDir
        fun provideCacheDir(@ApplicationContext context: Context): File = File(context.cacheDir, "downloads")

        @Provides
        @Singleton
        @SavedVideosPreferences
        fun provideSavedVideosDataStore(
            @ApplicationContext context: Context,
            @ApplicationScope scope: CoroutineScope,
        ): DataStore<Preferences> = PreferenceDataStoreFactory.create(scope = scope) {
            context.preferencesDataStoreFile("quoker_saved_videos")
        }
    }
}
