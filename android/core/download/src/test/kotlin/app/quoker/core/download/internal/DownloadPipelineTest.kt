package app.quoker.core.download.internal

import app.quoker.core.common.QuokerError
import app.quoker.core.data.model.PrepareResult
import app.quoker.core.data.model.TaskStatus
import app.quoker.core.download.DownloadErrors
import app.quoker.core.download.FakeVideoRepository
import app.quoker.core.download.FakeVideoSaver
import app.quoker.core.download.task
import app.quoker.core.network.ErrorCodes
import app.quoker.core.network.di.QuokerJson
import java.io.File
import java.io.IOException
import java.nio.file.Files
import kotlin.time.Duration.Companion.seconds
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.test.runTest
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okio.Buffer
import org.junit.After
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class DownloadPipelineTest {
    private val server = MockWebServer().apply { start() }
    private val dir: File = Files.createTempDirectory("pipeline").toFile()
    private val repo = FakeVideoRepository(server.url("/v1/videos/tiktok:1/file").toString())
    private val saver = FakeVideoSaver()
    private val pipeline = DownloadPipeline(repo, RangeFileFetcher(OkHttpClient(), QuokerJson, Dispatchers.IO), saver, dir)
    private val content = ByteArray(300) { it.toByte() }

    @After
    fun tearDown() {
        server.shutdown()
        dir.deleteRecursively()
    }

    private fun okFile() = MockResponse().setBody(Buffer().write(content))

    @Test
    fun readyFileIsSavedAndMarked() = runTest {
        server.enqueue(okFile())
        val progress = mutableListOf<DownloadProgress>()
        val outcome = pipeline.run("tiktok:1") { progress += it }
        assertEquals(DownloadOutcome.Saved("content://media/external/video/media/1"), outcome)
        assertArrayEquals(content, saver.saved.single().first)
        assertEquals("quoker_tiktok_1.mp4", saver.saved.single().second)
        assertEquals(1, repo.markCalls)
        assertEquals(DownloadProgress.Preparing, progress.first())
        assertEquals(DownloadProgress.Downloading(100), progress.last())
        assertTrue(dir.listFiles().orEmpty().isEmpty())
    }

    @Test
    fun pendingPrepareWaitsForTask() = runTest {
        repo.prepareResults.clear()
        repo.prepareResults += PrepareResult.Pending("t1")
        repo.taskStates = listOf(task(TaskStatus.RUNNING), task(TaskStatus.DONE))
        server.enqueue(okFile())
        assertTrue(pipeline.run("tiktok:1") {} is DownloadOutcome.Saved)
    }

    @Test
    fun failedTaskFailsWithItsError() = runTest {
        val error = QuokerError(ErrorCodes.PRIVATE_VIDEO, "private", retryable = false)
        repo.prepareResults.clear()
        repo.prepareResults += PrepareResult.Pending("t1")
        repo.taskStates = listOf(task(TaskStatus.FAILED, error))
        assertEquals(DownloadOutcome.Failed(error), pipeline.run("tiktok:1") {})
        assertEquals(0, server.requestCount)
    }

    @Test
    fun fileNotReadyAndTooManyStreamsAreRetried() = runTest {
        server.enqueue(MockResponse().setResponseCode(409).setBody("""{"error":{"code":"file_not_ready","message":"","retryable":false}}"""))
        server.enqueue(MockResponse().setResponseCode(429).setHeader("Retry-After", "3"))
        server.enqueue(okFile())
        val start = testScheduler.currentTime
        assertTrue(pipeline.run("tiktok:1") {} is DownloadOutcome.Saved)
        assertEquals(3, repo.prepareCalls)
        assertTrue(testScheduler.currentTime - start >= 3.seconds.inWholeMilliseconds)
    }

    @Test
    fun retryableErrorsGiveUpAfterMaxAttempts() = runTest {
        repeat(DownloadPipeline.MAX_ATTEMPTS) { server.enqueue(MockResponse().setResponseCode(503)) }
        val outcome = pipeline.run("tiktok:1") {}
        assertEquals(ErrorCodes.WORKER_UNAVAILABLE, (outcome as DownloadOutcome.Failed).error.code)
        assertEquals(DownloadPipeline.MAX_ATTEMPTS, server.requestCount)
    }

    @Test
    fun nonRetryablePrepareErrorFailsImmediately() = runTest {
        repo.prepareResults.clear()
        repo.prepareResults += QuokerError(ErrorCodes.NOT_FOUND, "", retryable = false)
        val outcome = pipeline.run("tiktok:1") {}
        assertEquals(ErrorCodes.NOT_FOUND, (outcome as DownloadOutcome.Failed).error.code)
        assertEquals(1, repo.prepareCalls)
    }

    @Test
    fun storageFailureMapsToStorageCode() = runTest {
        server.enqueue(okFile())
        saver.failure = IOException("no space")
        val outcome = pipeline.run("tiktok:1") {}
        assertEquals(DownloadErrors.STORAGE, (outcome as DownloadOutcome.Failed).error.code)
        assertEquals(0, repo.markCalls)
    }

    @Test
    fun markDownloadedFailureStillSaves() = runTest {
        server.enqueue(okFile())
        repo.markError = QuokerError(ErrorCodes.NETWORK, "", retryable = true)
        assertTrue(pipeline.run("tiktok:1") {} is DownloadOutcome.Saved)
        assertEquals(3, repo.markCalls)
    }

    @Test
    fun discardRemovesPartialFile() {
        val part = File(dir, "tiktok_1.part").apply { writeBytes(content) }
        pipeline.discard("tiktok:1")
        assertFalse(part.exists())
    }
}
