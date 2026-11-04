package app.quoker.core.update.di

import android.content.Context
import app.quoker.core.update.install.ApkInstaller
import app.quoker.core.update.install.SessionApkInstaller
import dagger.Binds
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.components.SingletonComponent
import java.io.File
import javax.inject.Qualifier

/** cacheDir/updates: where release APKs are downloaded. */
@Qualifier
@Retention(AnnotationRetention.BINARY)
internal annotation class UpdatesDir

@Module
@InstallIn(SingletonComponent::class)
internal abstract class UpdateModule {
    @Binds
    abstract fun bindInstaller(impl: SessionApkInstaller): ApkInstaller

    companion object {
        @Provides
        @UpdatesDir
        fun provideUpdatesDir(@ApplicationContext context: Context): File = File(context.cacheDir, "updates")
    }
}
