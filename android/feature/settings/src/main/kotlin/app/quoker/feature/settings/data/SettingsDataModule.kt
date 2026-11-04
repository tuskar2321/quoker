package app.quoker.feature.settings.data

import dagger.Binds
import dagger.Module
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent

@Module
@InstallIn(SingletonComponent::class)
internal abstract class SettingsDataModule {
    @Binds
    abstract fun bindCookiesUploadStore(impl: DataStoreCookiesUploadStore): CookiesUploadStore

    @Binds
    abstract fun bindCookiesFileReader(impl: ContentResolverCookiesFileReader): CookiesFileReader
}
