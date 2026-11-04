package app.quoker.core.download.di

import android.content.Context
import app.quoker.core.download.VideoDownloads
import app.quoker.core.download.WorkManagerVideoDownloads
import app.quoker.core.download.internal.DownloadCacheDir
import app.quoker.core.download.internal.MediaStoreVideoSaver
import app.quoker.core.download.internal.VideoSaver
import dagger.Binds
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.components.SingletonComponent
import java.io.File

@Module
@InstallIn(SingletonComponent::class)
internal abstract class DownloadModule {
    @Binds
    abstract fun bindVideoDownloads(impl: WorkManagerVideoDownloads): VideoDownloads

    @Binds
    abstract fun bindVideoSaver(impl: MediaStoreVideoSaver): VideoSaver

    companion object {
        @Provides
        @DownloadCacheDir
        fun provideCacheDir(@ApplicationContext context: Context): File = File(context.cacheDir, "downloads")
    }
}
