package app.quoker.core.download

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.work.ListenableWorker
import androidx.work.WorkerFactory
import androidx.work.WorkerParameters
import androidx.work.testing.TestListenableWorkerBuilder
import androidx.work.workDataOf
import app.quoker.core.common.QuokerError
import app.quoker.core.download.DownloadWorker.Companion.toError
import app.quoker.core.download.internal.DownloadNotifier
import app.quoker.core.download.internal.DownloadPipeline
import app.quoker.core.download.internal.RangeFileFetcher
import app.quoker.core.network.di.QuokerJson
import java.io.File
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.test.runTest
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class DownloadWorkerTest {
    private val context = ApplicationProvider.getApplicationContext<Context>()
    private val server = MockWebServer().apply { start() }
    private val repo = FakeVideoRepository(server.url("/file").toString())
    private val saver = FakeVideoSaver()

    @After
    fun tearDown() = server.shutdown()

    private fun worker(videoId: String?): DownloadWorker {
        val pipeline = DownloadPipeline(
            repo,
            RangeFileFetcher(OkHttpClient(), QuokerJson, Dispatchers.IO),
            saver,
            File(context.cacheDir, "downloads"),
        )
        val factory = object : WorkerFactory() {
            override fun createWorker(appContext: Context, workerClassName: String, params: WorkerParameters): ListenableWorker =
                DownloadWorker(appContext, params, pipeline, DownloadNotifier(appContext))
        }
        val input = if (videoId == null) workDataOf() else workDataOf(DownloadWorker.KEY_VIDEO_ID to videoId, DownloadWorker.KEY_TITLE to "@a")
        return TestListenableWorkerBuilder<DownloadWorker>(context, input).setWorkerFactory(factory).build()
    }

    @Test
    fun successReturnsContentUri() = runTest {
        server.enqueue(MockResponse().setBody("video"))
        val result = worker("tiktok:1").doWork()
        assertEquals(
            ListenableWorker.Result.success(workDataOf(DownloadWorker.KEY_URI to "content://media/external/video/media/1")),
            result,
        )
    }

    @Test
    fun failureCarriesErrorCode() = runTest {
        repo.prepareResults.clear()
        repo.prepareResults += QuokerError("private_video", "m", retryable = false)
        val result = worker("tiktok:1").doWork() as ListenableWorker.Result.Failure
        assertEquals("private_video", result.outputData.toError().code)
    }

    @Test
    fun missingVideoIdFails() = runTest {
        val result = worker(null).doWork() as ListenableWorker.Result.Failure
        assertEquals("invalid_request", result.outputData.toError().code)
    }
}
