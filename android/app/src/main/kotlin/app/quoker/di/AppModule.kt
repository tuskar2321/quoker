package app.quoker.di

import app.quoker.BuildConfig
import app.quoker.core.common.AppInfo
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import javax.inject.Singleton

@Module
@InstallIn(SingletonComponent::class)
object AppModule {
    @Provides
    @Singleton
    fun provideAppInfo(): AppInfo = AppInfo(
        apiBaseUrl = BuildConfig.API_BASE_URL,
        versionCode = BuildConfig.VERSION_CODE,
        versionName = BuildConfig.VERSION_NAME,
    )
}
