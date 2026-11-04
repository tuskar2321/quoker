package app.quoker.core.data.di

import android.content.Context
import androidx.datastore.core.DataStore
import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.preferencesDataStoreFile
import androidx.room.Room
import app.quoker.core.common.di.IoDispatcher
import app.quoker.core.data.db.HistoryDao
import app.quoker.core.data.db.QuokerDatabase
import app.quoker.core.data.db.TargetDao
import app.quoker.core.data.repository.AdminRepository
import app.quoker.core.data.repository.AdminRepositoryImpl
import app.quoker.core.data.repository.AppVersionRepository
import app.quoker.core.data.repository.AppVersionRepositoryImpl
import app.quoker.core.data.repository.AuthRepository
import app.quoker.core.data.repository.AuthRepositoryImpl
import app.quoker.core.data.repository.HistoryRepository
import app.quoker.core.data.repository.HistoryRepositoryImpl
import app.quoker.core.data.repository.SendsRepository
import app.quoker.core.data.repository.SendsRepositoryImpl
import app.quoker.core.data.repository.TargetsRepository
import app.quoker.core.data.repository.TargetsRepositoryImpl
import app.quoker.core.data.repository.VideoRepository
import app.quoker.core.data.repository.VideoRepositoryImpl
import app.quoker.core.data.session.CachedDataCleaner
import app.quoker.core.data.session.DataStoreSessionStore
import app.quoker.core.data.session.SessionStore
import app.quoker.core.data.session.TinkTokenCipher
import app.quoker.core.data.session.TokenCipher
import app.quoker.core.network.DeviceIdProvider
import app.quoker.core.network.SessionEvents
import app.quoker.core.network.SessionTokenProvider
import dagger.Binds
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.components.SingletonComponent
import javax.inject.Qualifier
import javax.inject.Singleton
import kotlinx.coroutines.CoroutineDispatcher
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.withContext

/** Process-wide scope for work that outlives screens (session event handling). */
@Qualifier
@Retention(AnnotationRetention.BINARY)
annotation class ApplicationScope

/** DataStore for non-secret app preferences (e.g. the last update check). */
@Qualifier
@Retention(AnnotationRetention.BINARY)
annotation class AppPreferences

@Qualifier
@Retention(AnnotationRetention.BINARY)
annotation class SessionPreferences

@Module
@InstallIn(SingletonComponent::class)
object DataModule {
    @Provides
    @Singleton
    @ApplicationScope
    fun provideApplicationScope(@IoDispatcher io: CoroutineDispatcher): CoroutineScope =
        CoroutineScope(SupervisorJob() + io)

    @Provides
    @Singleton
    @SessionPreferences
    fun provideSessionDataStore(
        @ApplicationContext context: Context,
        @ApplicationScope scope: CoroutineScope,
    ): DataStore<Preferences> = PreferenceDataStoreFactory.create(scope = scope) {
        context.preferencesDataStoreFile("quoker_session")
    }

    @Provides
    @Singleton
    @AppPreferences
    fun provideAppDataStore(
        @ApplicationContext context: Context,
        @ApplicationScope scope: CoroutineScope,
    ): DataStore<Preferences> = PreferenceDataStoreFactory.create(scope = scope) {
        context.preferencesDataStoreFile("quoker_app")
    }

    @Provides
    @Singleton
    fun provideTokenCipher(@ApplicationContext context: Context): TokenCipher = TinkTokenCipher(context)

    @Provides
    @Singleton
    fun provideDatabase(@ApplicationContext context: Context): QuokerDatabase =
        Room.databaseBuilder(context, QuokerDatabase::class.java, QuokerDatabase.NAME)
            // Pure cache: losing it on a schema change is fine.
            .fallbackToDestructiveMigration(dropAllTables = true)
            .build()

    @Provides
    fun provideTargetDao(db: QuokerDatabase): TargetDao = db.targetDao()

    @Provides
    fun provideHistoryDao(db: QuokerDatabase): HistoryDao = db.historyDao()

    @Provides
    @Singleton
    fun provideCachedDataCleaner(db: QuokerDatabase, @IoDispatcher io: CoroutineDispatcher): CachedDataCleaner =
        CachedDataCleaner { withContext(io) { db.clearAllTables() } }

    @Provides
    @Singleton
    fun provideSessionStore(
        @SessionPreferences dataStore: DataStore<Preferences>,
        cipher: TokenCipher,
        cleaner: CachedDataCleaner,
        events: SessionEvents,
        @ApplicationScope scope: CoroutineScope,
    ): SessionStore = DataStoreSessionStore(dataStore, cipher, cleaner, events, scope)
}

@Module
@InstallIn(SingletonComponent::class)
internal abstract class DataBindingsModule {
    @Binds
    abstract fun bindSessionTokenProvider(store: SessionStore): SessionTokenProvider

    @Binds
    abstract fun bindDeviceIdProvider(store: SessionStore): DeviceIdProvider

    @Binds
    @Singleton
    abstract fun bindAuthRepository(impl: AuthRepositoryImpl): AuthRepository

    @Binds
    @Singleton
    abstract fun bindVideoRepository(impl: VideoRepositoryImpl): VideoRepository

    @Binds
    @Singleton
    abstract fun bindTargetsRepository(impl: TargetsRepositoryImpl): TargetsRepository

    @Binds
    @Singleton
    abstract fun bindSendsRepository(impl: SendsRepositoryImpl): SendsRepository

    @Binds
    @Singleton
    abstract fun bindHistoryRepository(impl: HistoryRepositoryImpl): HistoryRepository

    @Binds
    @Singleton
    abstract fun bindAdminRepository(impl: AdminRepositoryImpl): AdminRepository

    @Binds
    @Singleton
    abstract fun bindAppVersionRepository(impl: AppVersionRepositoryImpl): AppVersionRepository
}
