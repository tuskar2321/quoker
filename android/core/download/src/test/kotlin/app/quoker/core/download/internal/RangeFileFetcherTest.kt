package app.quoker.core.download.internal

import app.quoker.core.network.ErrorCodes
import app.quoker.core.network.QuokerException
import app.quoker.core.network.di.QuokerJson
import java.io.File
import java.nio.file.Files
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.test.runTest
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okio.Buffer
import org.junit.After
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test

class RangeFileFetcherTest {
    private val server = MockWebServer().apply { start() }
    private val dir: File = Files.createTempDirectory("fetch").toFile()
    private val target = File(dir, "v.part")
    private val fetcher = RangeFileFetcher(OkHttpClient(), QuokerJson, Dispatchers.IO)
    private val content = ByteArray(1000) { (it % 251).toByte() }
    private val url get() = server.url("/v1/videos/tiktok:1/file").toString()

    @After
    fun tearDown() {
        server.shutdown()
        dir.deleteRecursively()
    }

    @Test
    fun fullDownloadWithoutRange() = runTest {
        server.enqueue(MockResponse().setBody(Buffer().write(content)))
        val progress = mutableListOf<Long>()
        fetcher.fetch(url, target) { done, total ->
            assertEquals(1000L, total)
            progress += done
        }
        assertArrayEquals(content, target.readBytes())
        assertNull(server.takeRequest().getHeader("Range"))
        assertEquals(1000L, progress.last())
    }

    @Test
    fun resumesPartialFileWithRange() = runTest {
        target.writeBytes(content.copyOfRange(0, 400))
        server.enqueue(
            MockResponse().setResponseCode(206)
                .setHeader("Content-Range", "bytes 400-999/1000")
                .setBody(Buffer().write(content.copyOfRange(400, 1000))),
        )
        fetcher.fetch(url, target) { _, _ -> }
        assertEquals("bytes=400-", server.takeRequest().getHeader("Range"))
        assertArrayEquals(content, target.readBytes())
    }

    @Test
    fun serverIgnoringRangeRestartsFile() = runTest {
        target.writeBytes(ByteArray(400) { 9 })
        server.enqueue(MockResponse().setBody(Buffer().write(content)))
        fetcher.fetch(url, target) { _, _ -> }
        assertArrayEquals(content, target.readBytes())
    }

    @Test
    fun rangeNotSatisfiableWithCompleteFileIsDone() = runTest {
        target.writeBytes(content)
        server.enqueue(MockResponse().setResponseCode(416).setHeader("Content-Range", "bytes */1000"))
        fetcher.fetch(url, target) { _, _ -> }
        assertArrayEquals(content, target.readBytes())
        assertEquals(1, server.requestCount)
    }

    @Test
    fun staleRangeRestartsWithoutRange() = runTest {
        target.writeBytes(ByteArray(1200))
        server.enqueue(MockResponse().setResponseCode(416).setHeader("Content-Range", "bytes */1000"))
        server.enqueue(MockResponse().setBody(Buffer().write(content)))
        fetcher.fetch(url, target) { _, _ -> }
        server.takeRequest()
        assertNull(server.takeRequest().getHeader("Range"))
        assertArrayEquals(content, target.readBytes())
    }

    @Test
    fun errorStatusesBecomeQuokerErrors() = runTest {
        data class Case(val status: Int, val body: String, val code: String, val retryable: Boolean)
        val cases = listOf(
            Case(409, """{"error":{"code":"file_not_ready","message":"m","retryable":false}}""", ErrorCodes.FILE_NOT_READY, true),
            Case(429, "", ErrorCodes.RATE_LIMITED, true),
            Case(404, """{"error":{"code":"not_found","message":"m","retryable":false}}""", ErrorCodes.NOT_FOUND, false),
            Case(503, "<html>", ErrorCodes.WORKER_UNAVAILABLE, true),
        )
        for (case in cases) {
            server.enqueue(MockResponse().setResponseCode(case.status).setHeader("Retry-After", "7").setBody(case.body))
            try {
                fetcher.fetch(url, target) { _, _ -> }
                fail("expected failure for ${case.status}")
            } catch (e: QuokerException) {
                assertEquals(case.code, e.code)
                assertEquals("status ${case.status}", case.retryable, e.error.retryable)
                assertEquals(7L, e.retryAfterSeconds)
                assertEquals(case.status, e.httpStatus)
            }
        }
    }

    @Test
    fun parsesContentRange() {
        assertEquals(ContentRange(400, 1000), ContentRange.parse("bytes 400-999/1000"))
        assertEquals(ContentRange(null, 1000), ContentRange.parse("bytes */1000"))
        assertNull(ContentRange.parse("items 1-2/3"))
        assertTrue(ContentRange.parse("bytes 0-9/*")!!.total == null)
    }
}
