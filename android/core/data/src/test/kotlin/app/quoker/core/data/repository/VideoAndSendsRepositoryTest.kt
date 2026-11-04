package app.quoker.core.data.repository

import app.cash.turbine.test
import app.quoker.core.data.TestApi
import app.quoker.core.data.Wire
import app.quoker.core.data.model.FileStatus
import app.quoker.core.data.model.Platform
import app.quoker.core.data.model.PrepareResult
import app.quoker.core.data.model.ResolveResult
import app.quoker.core.data.model.SendStatus
import app.quoker.core.data.model.TaskStatus
import app.quoker.core.network.QuokerException
import kotlinx.coroutines.test.currentTime
import kotlinx.coroutines.test.runTest
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test

class VideoAndSendsRepositoryTest {
    private val t = TestApi()
    private val videos = VideoRepositoryImpl(t.api, t.urls)
    private val sends = SendsRepositoryImpl(t.api)

    @After
    fun tearDown() = t.close()

    @Test
    fun resolveReady() = runTest {
        t.enqueue(200, """{"video":${Wire.VIDEO}}""")
        val res = videos.resolve("https://vm.tiktok.com/x/") as ResolveResult.Ready
        assertEquals(Platform.TIKTOK, res.video.platform)
        assertEquals(27L, res.video.durationSeconds)
        assertEquals(t.urls.thumbnail("tiktok:1"), res.video.thumbnailUrl)
        assertEquals(FileStatus.NONE, res.video.file.status)
        assertEquals("""{"url":"https://vm.tiktok.com/x/"}""", t.server.takeRequest().body.readUtf8())
    }

    @Test
    fun resolveBareVideo() = runTest {
        t.enqueue(200, """{"video":${Wire.VIDEO_BARE}}""")
        val v = (videos.resolve("u") as ResolveResult.Ready).video
        assertNull(v.thumbnailUrl)
        assertNull(v.noWatermark)
        assertEquals(Platform.INSTAGRAM, v.platform)
    }

    @Test
    fun resolvePendingThenPollUntilDone() = runTest {
        t.enqueue(202, """{"task_id":"t_1"}""")
        assertEquals(ResolveResult.Pending("t_1"), videos.resolve("u"))

        t.enqueue(200, Wire.task("queued"))
        t.enqueue(200, Wire.task("running", progress = 40))
        t.enqueue(200, Wire.task("running", progress = 40)) // unchanged: not re-emitted
        t.enqueue(200, Wire.task("done", progress = 100, video = Wire.VIDEO))
        val start = currentTime
        videos.pollTask("t_1").test {
            assertEquals(TaskStatus.QUEUED, awaitItem().status)
            assertEquals(40, awaitItem().progress)
            val done = awaitItem()
            assertEquals(TaskStatus.DONE, done.status)
            assertEquals("tiktok:1", done.video?.id)
            awaitComplete()
        }
        assertEquals(3_000L, currentTime - start)
        t.server.takeRequest()
        assertEquals("/v1/tasks/t_1", t.server.takeRequest().path)
    }

    @Test
    fun pollTaskEndsOnFailureWithError() = runTest {
        t.enqueue(200, Wire.task("failed", error = """{"code":"private_video","message":"m","retryable":false}"""))
        videos.pollTask("t_1").test {
            val failed = awaitItem()
            assertEquals(TaskStatus.FAILED, failed.status)
            assertEquals("private_video", failed.error?.code)
            awaitComplete()
        }
    }

    @Test
    fun pollToleratesTransientErrors() = runTest {
        t.enqueue(503, Wire.error("worker_unavailable", retryable = true))
        t.enqueue(200, Wire.task("done", video = Wire.VIDEO))
        videos.pollTask("t_1").test {
            assertEquals(TaskStatus.DONE, awaitItem().status)
            awaitComplete()
        }
    }

    @Test
    fun pollFailsOnPermanentError() = runTest {
        t.enqueue(404, Wire.error("not_found"))
        videos.pollTask("t_1").test {
            assertEquals("not_found", (awaitError() as QuokerException).code)
        }
    }

    @Test
    fun pollGivesUpAfterRepeatedTransientErrors() = runTest {
        repeat(MAX_TRANSIENT_POLL_FAILURES) { t.enqueue(502, Wire.error("upstream_error", retryable = true)) }
        videos.pollTask("t_1").test {
            assertEquals("upstream_error", (awaitError() as QuokerException).code)
        }
    }

    @Test
    fun prepare() = runTest {
        t.enqueue(200, """{"file":{"status":"ready","size_bytes":100}}""")
        assertEquals(PrepareResult.Ready(100), videos.prepare("tiktok:1"))
        t.enqueue(202, """{"task_id":"t_2"}""")
        assertEquals(PrepareResult.Pending("t_2"), videos.prepare("tiktok:1"))
        assertEquals("/v1/videos/tiktok:1/prepare", t.server.takeRequest().path)
    }

    @Test
    fun markDownloadedAndUrls() = runTest {
        t.enqueue(204)
        videos.markDownloaded("tiktok:1")
        assertEquals("POST /v1/videos/tiktok:1/downloaded", t.server.takeRequest().let { "${it.method} ${it.path}" })
        assertTrue(videos.fileUrl("tiktok:1").endsWith("/v1/videos/tiktok:1/file"))
    }

    @Test
    fun errorsAreThrown() = runTest {
        t.enqueue(422, Wire.error("unsupported_url"))
        try {
            videos.resolve("https://example.com")
            fail()
        } catch (e: QuokerException) {
            assertEquals("unsupported_url", e.code)
        }
    }

    @Test
    fun createSendUsesIdempotencyKeyAndPolls() = runTest {
        val key = sends.newIdempotencyKey()
        t.enqueue(202, Wire.send("queued"))
        val send = sends.create("tiktok:1", 42, caption = null, signAsUser = true, idempotencyKey = key)
        assertEquals(SendStatus.QUEUED, send.status)
        val req = t.server.takeRequest()
        assertEquals(key, req.getHeader("Idempotency-Key"))
        assertEquals("""{"video_id":"tiktok:1","target_id":42,"caption":null,"sign_as_user":true}""", req.body.readUtf8())

        t.enqueue(200, Wire.send("sending"))
        t.enqueue(200, Wire.send("sent"))
        val start = currentTime
        sends.poll(1001).test {
            assertEquals(SendStatus.SENDING, awaitItem().status)
            assertEquals(SendStatus.SENT, awaitItem().status)
            awaitComplete()
        }
        assertEquals(1_000L, currentTime - start)
    }

    @Test
    fun sendFailure() = runTest {
        t.enqueue(200, Wire.send("failed", """{"code":"target_inactive","message":"m","retryable":false}"""))
        val s = sends.get(1001)
        assertEquals(SendStatus.FAILED, s.status)
        assertEquals("target_inactive", s.error?.code)
    }
}
