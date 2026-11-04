package app.quoker.core.network.di

import app.quoker.core.network.ApiUrls
import app.quoker.core.network.AuthHeaderInterceptor
import app.quoker.core.network.ErrorMappingInterceptor
import app.quoker.core.network.QuokerApi
import app.quoker.core.network.SessionSignalInterceptor
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.components.SingletonComponent
import java.util.concurrent.TimeUnit
import javax.inject.Qualifier
import javax.inject.Singleton
import kotlinx.serialization.json.Json
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import retrofit2.Retrofit
import retrofit2.converter.kotlinx.serialization.asConverterFactory

/**
 * OkHttp client for binary GETs on the API origin (thumbnails via Coil's OkHttp fetcher, video
 * files with `Range`): session headers and session events, but non-2xx responses are returned,
 * not thrown.
 */
@Qualifier
@Retention(AnnotationRetention.BINARY)
annotation class MediaHttpClient

@Module
@InstallIn(SingletonComponent::class)
object NetworkModule {
    @Provides
    @Singleton
    fun provideJson(): Json = QuokerJson

    @Provides
    @Singleton
    fun provideOkHttpClient(
        headers: AuthHeaderInterceptor,
        errors: ErrorMappingInterceptor,
    ): OkHttpClient = baseClientBuilder()
        .addInterceptor(headers)
        .addInterceptor(errors)
        .build()

    @Provides
    @Singleton
    @MediaHttpClient
    fun provideMediaOkHttpClient(
        api: OkHttpClient,
        headers: AuthHeaderInterceptor,
        signals: SessionSignalInterceptor,
    ): OkHttpClient = api.newBuilder()
        .apply { interceptors().clear() }
        .addInterceptor(headers)
        .addInterceptor(signals)
        .build()

    @Provides
    @Singleton
    fun provideRetrofit(client: OkHttpClient, json: Json, urls: ApiUrls): Retrofit =
        createRetrofit(urls.baseUrl.toString(), client, json)

    @Provides
    @Singleton
    fun provideQuokerApi(retrofit: Retrofit): QuokerApi = retrofit.create(QuokerApi::class.java)
}

val QuokerJson: Json = Json {
    ignoreUnknownKeys = true
    explicitNulls = true
    coerceInputValues = true
}

/** Timeouts from docs/08-android.md: connect 10 s, read 30 s, write 30 s. */
fun baseClientBuilder(): OkHttpClient.Builder = OkHttpClient.Builder()
    .connectTimeout(10, TimeUnit.SECONDS)
    .readTimeout(30, TimeUnit.SECONDS)
    .writeTimeout(30, TimeUnit.SECONDS)

fun createRetrofit(baseUrl: String, client: OkHttpClient, json: Json): Retrofit = Retrofit.Builder()
    .baseUrl(baseUrl)
    .client(client)
    .addConverterFactory(json.asConverterFactory("application/json; charset=utf-8".toMediaType()))
    .build()
