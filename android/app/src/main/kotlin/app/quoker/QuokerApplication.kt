package app.quoker

import android.app.Application
import android.content.Context
import androidx.hilt.work.HiltWorkerFactory
import androidx.work.Configuration
import app.quoker.core.network.di.MediaHttpClient
import coil3.ImageLoader
import coil3.PlatformContext
import coil3.SingletonImageLoader
import coil3.network.okhttp.OkHttpNetworkFetcherFactory
import dagger.Lazy
import dagger.hilt.android.HiltAndroidApp
import javax.inject.Inject
import okhttp3.OkHttpClient

@HiltAndroidApp
class QuokerApplication : Application(), Configuration.Provider, SingletonImageLoader.Factory {
    override fun attachBaseContext(newBase: Context) {
        super.attachBaseContext(AppLocale.wrap(newBase))
    }

    @Inject
    lateinit var workerFactory: HiltWorkerFactory

    // Thumbnails need the session headers, so Coil goes through the media client.
    @Inject
    @field:MediaHttpClient
    lateinit var mediaClient: Lazy<OkHttpClient>

    override val workManagerConfiguration: Configuration
        get() = Configuration.Builder().setWorkerFactory(workerFactory).build()

    override fun newImageLoader(context: PlatformContext): ImageLoader =
        ImageLoader.Builder(context)
            .components { add(OkHttpNetworkFetcherFactory(callFactory = { mediaClient.get() })) }
            .build()
}
