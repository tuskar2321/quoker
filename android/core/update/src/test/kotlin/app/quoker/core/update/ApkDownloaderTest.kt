package app.quoker.core.update

import app.quoker.core.common.AppInfo
import app.quoker.core.network.ApiUrls
import app.quoker.core.network.QuokerException
import java.io.File
import java.nio.file.Files
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.runTest
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okio.Buffer
import org.junit.After
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test

class ApkDownloaderTest {
    private val server = MockWebServer().apply { start() }
    private val dir: File = Files.createTempDirectory("updates").toFile()
    private val apk = ByteArray(200_000) { (it % 251).toByte() }

    @After
    fun tearDown() {
        server.shutdown()
        dir.deleteRecursively()
    }

    private fun downloader(scheduler: kotlinx.coroutines.test.TestCoroutineScheduler) = ApkDownloader(
        client = OkHttpClient(),
        urls = ApiUrls(AppInfo(server.url("/").toString(), INSTALLED_VERSION, "1.10")),
        dir = dir,
        io = StandardTestDispatcher(scheduler),
    )

    @Test
    fun `fresh download verifies sha256 and reports progress`() = runTest {
        server.enqueue(MockResponse().setBody(Buffer().write(apk)))
        val rel = release(sha256 = sha256Hex(apk))
        val progress = mutableListOf<Int>()

        val file = downloader(testScheduler).download(rel) { progress += it }

        assertArrayEquals(apk, file.readBytes())
        assertEquals(100, progress.last())
        assertNull(server.takeRequest().getHeader("Range"))
    }

    @Test
    fun `partial file is resumed with Range and If-Range`() = runTest {
        val rel = release(sha256 = sha256Hex(apk))
        val offset = 75_000
        File(dir, "quoker-${rel.versionCode}.apk.part").writeBytes(apk.copyOfRange(0, offset))
        server.enqueue(
            MockResponse()
                .setResponseCode(206)
                .setHeader("Content-Range", "bytes $offset-${apk.size - 1}/${apk.size}")
                .setBody(Buffer().write(apk, offset, apk.size - offset)),
        )
        val progress = mutableListOf<Int>()

        val file = downloader(testScheduler).download(rel) { progress += it }

        val request = server.takeRequest()
        assertEquals("bytes=$offset-", request.getHeader("Range"))
        assertEquals("\"${rel.sha256}\"", request.getHeader("If-Range"))
        assertArrayEquals(apk, file.readBytes())
        assertTrue("progress starts past the resumed part", progress.first() >= offset * 100 / apk.size)
    }

    @Test
    fun `server ignoring Range restarts from zero`() = runTest {
        val rel = release(sha256 = sha256Hex(apk))
        File(dir, "quoker-${rel.versionCode}.apk.part").writeBytes(ByteArray(1000) { 7 })
        server.enqueue(MockResponse().setBody(Buffer().write(apk)))

        val file = downloader(testScheduler).download(rel) {}

        assertArrayEquals(apk, file.readBytes())
    }

    @Test
    fun `sha256 mismatch fails and removes the file`() = runTest {
        server.enqueue(MockResponse().setBody(Buffer().write(apk)))
        val rel = release(sha256 = sha256Hex(byteArrayOf(1, 2, 3)))

        try {
            downloader(testScheduler).download(rel) {}
            fail("expected a checksum failure")
        } catch (e: QuokerException) {
            assertEquals(UpdateErrorCodes.CHECKSUM_MISMATCH, e.code)
        }
        assertFalse(File(dir, "quoker-${rel.versionCode}.apk").exists())
        assertFalse(File(dir, "quoker-${rel.versionCode}.apk.part").exists())
    }

    @Test
    fun `already downloaded apk is reused without a request`() = runTest {
        val rel = release(sha256 = sha256Hex(apk))
        File(dir.apply { mkdirs() }, "quoker-${rel.versionCode}.apk").writeBytes(apk)
        File(dir, "quoker-9.apk").writeBytes(byteArrayOf(1))

        val file = downloader(testScheduler).download(rel) {}

        assertArrayEquals(apk, file.readBytes())
        assertEquals(0, server.requestCount)
        assertFalse("stale versions are removed", File(dir, "quoker-9.apk").exists())
    }

    @Test
    fun `http 404 maps to no release`() = runTest {
        server.enqueue(MockResponse().setResponseCode(404))
        try {
            downloader(testScheduler).download(release()) {}
            fail("expected a failure")
        } catch (e: QuokerException) {
            assertEquals(UpdateErrorCodes.NO_RELEASE, e.code)
        }
    }
}
