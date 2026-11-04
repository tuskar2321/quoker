package app.quoker.core.network

import app.cash.turbine.test
import app.quoker.core.common.AppInfo
import app.quoker.core.network.di.NetworkModule
import app.quoker.core.network.di.QuokerJson
import app.quoker.core.network.model.AllowWorkerRequestDto
import app.quoker.core.network.model.CreateInviteRequestDto
import app.quoker.core.network.model.CreateSendRequestDto
import app.quoker.core.network.model.PatchTargetRequestDto
import app.quoker.core.network.model.RegisterRequestDto
import app.quoker.core.network.model.ResolveRequestDto
import kotlinx.coroutines.test.runTest
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.mockwebserver.SocketPolicy
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Before
import org.junit.Test

class QuokerApiTest {
    private val server = MockWebServer()
    private var token: String? = "tok"
    private val events = SessionEvents()
    private lateinit var api: QuokerApi
    private lateinit var urls: ApiUrls
    private lateinit var media: OkHttpClient

    @Before
    fun setUp() {
        server.start()
        val appInfo = AppInfo(server.url("/").toString().removeSuffix("/"), versionCode = 42, versionName = "1.0")
        urls = ApiUrls(appInfo)
        val headers = AuthHeaderInterceptor(appInfo, urls, { token }, { DEVICE_ID })
        val client = NetworkModule.provideOkHttpClient(headers, ErrorMappingInterceptor(QuokerJson, events))
        media = NetworkModule.provideMediaOkHttpClient(client, headers, SessionSignalInterceptor(QuokerJson, events))
        api = NetworkModule.provideQuokerApi(NetworkModule.provideRetrofit(client, QuokerJson, urls))
    }

    @After
    fun tearDown() {
        server.shutdown()
    }

    private fun enqueue(code: Int, body: String? = null, headers: Map<String, String> = emptyMap()) {
        val r = MockResponse().setResponseCode(code)
        if (body != null) r.setBody(body).setHeader("Content-Type", "application/json; charset=utf-8")
        headers.forEach { (k, v) -> r.setHeader(k, v) }
        server.enqueue(r)
    }

    private suspend fun expectError(block: suspend () -> Unit): QuokerException {
        try {
            block()
        } catch (e: QuokerException) {
            return e
        }
        fail("expected QuokerException")
        error("unreachable")
    }

    @Test
    fun addsSessionHeaders() = runTest {
        enqueue(200, Samples.ME)
        api.me()
        val req = server.takeRequest()
        assertEquals("Bearer tok", req.getHeader("Authorization"))
        assertEquals("42", req.getHeader("X-Quoker-Version-Code"))
        assertEquals(DEVICE_ID, req.getHeader("X-Device-Id"))
        assertEquals("/v1/me", req.path)
    }

    @Test
    fun omitsAuthorizationWithoutSession() = runTest {
        token = null
        enqueue(201, Samples.REGISTER)
        val res = api.register(RegisterRequestDto("K7Q2-ZX9P", DEVICE_ID, "Pixel"))
        assertEquals("s3cr3t-token", res.sessionToken)
        val req = server.takeRequest()
        assertNull(req.getHeader("Authorization"))
        assertEquals("42", req.getHeader("X-Quoker-Version-Code"))
        assertEquals(
            """{"invite_code":"K7Q2-ZX9P","device_id":"$DEVICE_ID","device_name":"Pixel"}""",
            req.body.readUtf8(),
        )
    }

    @Test
    fun headersNeverGoToOtherHosts() {
        val other = MockWebServer().apply { start() }
        try {
            other.enqueue(MockResponse().setResponseCode(200))
            // Same host, different port: a different origin.
            media.newCall(Request.Builder().url(other.url("/x.jpg")).build()).execute().close()
            val req = other.takeRequest()
            assertNull(req.getHeader("Authorization"))
            assertNull(req.getHeader("X-Device-Id"))
        } finally {
            other.shutdown()
        }
    }

    @Test
    fun mediaClientAuthenticatesThumbnails() {
        server.enqueue(MockResponse().setResponseCode(200).setBody("jpeg"))
        val url = urls.absolute("/v1/videos/tiktok:1/thumbnail")
        assertEquals(urls.thumbnail("tiktok:1"), url)
        media.newCall(Request.Builder().url(url).build()).execute().use { assertEquals("jpeg", it.body.string()) }
        val req = server.takeRequest()
        assertEquals("/v1/videos/tiktok:1/thumbnail", req.path)
        assertEquals("Bearer tok", req.getHeader("Authorization"))
    }

    @Test
    fun mediaClientReturnsErrorsButSignalsSession() = runTest {
        events.events.test {
            enqueue(401, Samples.error("unauthorized"))
            media.newCall(Request.Builder().url(urls.file("tiktok:1")).build()).execute().use {
                assertEquals(401, it.code)
                assertTrue(it.body.string().contains("unauthorized"))
            }
            assertEquals(SessionEvent.SignedOut, awaitItem())
        }
    }

    @Test
    fun mapsErrorBody() = runTest {
        enqueue(404, Samples.error("private_video", "Видео приватное или удалено"))
        val e = expectError { api.resolve(ResolveRequestDto("https://vm.tiktok.com/x")) }
        assertEquals("private_video", e.code)
        assertEquals("Видео приватное или удалено", e.error.message)
        assertEquals(404, e.httpStatus)
        assertNull(e.retryAfterSeconds)
    }

    @Test
    fun unauthorizedSignsOut() = runTest {
        events.events.test {
            enqueue(401, Samples.error("unauthorized"))
            assertEquals("unauthorized", expectError { api.me() }.code)
            assertEquals(SessionEvent.SignedOut, awaitItem())
        }
    }

    @Test
    fun unauthorizedWithoutTokenDoesNotSignOut() = runTest {
        token = null
        events.events.test {
            enqueue(401, Samples.error("unauthorized"))
            expectError { api.me() }
            expectNoEvents()
        }
    }

    @Test
    fun upgradeRequired() = runTest {
        events.events.test {
            enqueue(426, Samples.error("upgrade_required"))
            assertEquals("upgrade_required", expectError { api.history() }.code)
            assertEquals(SessionEvent.UpgradeRequired, awaitItem())
        }
    }

    @Test
    fun blocked() = runTest {
        events.events.test {
            enqueue(403, Samples.error("user_blocked"))
            assertEquals("user_blocked", expectError { api.me() }.code)
            assertEquals(SessionEvent.Blocked, awaitItem())
            enqueue(403, Samples.error("forbidden"))
            assertEquals("forbidden", expectError { api.adminStatus() }.code)
            expectNoEvents()
        }
    }

    @Test
    fun rateLimitedCarriesRetryAfter() = runTest {
        enqueue(429, Samples.error("rate_limited", retryable = true), mapOf("Retry-After" to "37"))
        val e = expectError { api.resolve(ResolveRequestDto("u")) }
        assertEquals("rate_limited", e.code)
        assertTrue(e.error.retryable)
        assertEquals(37L, e.retryAfterSeconds)
    }

    @Test
    fun serverErrorWithoutBody() = runTest {
        enqueue(500)
        val e = expectError { api.me() }
        assertEquals("internal", e.code)
        assertTrue(e.error.retryable)
        assertEquals(500, e.httpStatus)
    }

    @Test
    fun proxyErrorPage() = runTest {
        server.enqueue(MockResponse().setResponseCode(502).setBody("<html>Bad Gateway</html>"))
        assertEquals("upstream_error", expectError { api.me() }.code)
    }

    @Test
    fun networkFailure() = runTest {
        server.enqueue(MockResponse().setSocketPolicy(SocketPolicy.DISCONNECT_AT_START))
        val e = expectError { api.me() }
        assertEquals(ErrorCodes.NETWORK, e.code)
        assertTrue(e.error.retryable)
        assertNull(e.httpStatus)
    }

    @Test
    fun malformedBodyBecomesInternal() = runTest {
        enqueue(200, """{"id":"x"}""")
        val e = expectError { quokerCall { api.me() } }
        assertEquals(ErrorCodes.INTERNAL, e.code)
    }

    @Test
    fun sendCarriesIdempotencyKey() = runTest {
        enqueue(202, Samples.SEND_QUEUED)
        val send = api.createSend("3b1d7e8e-0000-4000-8000-000000000001", CreateSendRequestDto("tiktok:1", 42, "hi", true)).send
        assertEquals(1001L, send.id)
        val req = server.takeRequest()
        assertEquals("POST", req.method)
        assertEquals("/v1/sends", req.path)
        assertEquals("3b1d7e8e-0000-4000-8000-000000000001", req.getHeader("Idempotency-Key"))
        assertEquals("""{"video_id":"tiktok:1","target_id":42,"caption":"hi","sign_as_user":true}""", req.body.readUtf8())
    }

    @Test
    fun routes() = runTest {
        data class Case(val call: suspend () -> Unit, val code: Int, val body: String?, val method: String, val path: String)

        val cases = listOf(
            Case({ api.logout() }, 204, null, "POST", "/v1/auth/logout"),
            Case({ api.createTelegramLink() }, 200, Samples.TELEGRAM_LINK, "POST", "/v1/auth/telegram/link"),
            Case({ api.telegramStatus() }, 200, Samples.TELEGRAM_STATUS_PENDING, "GET", "/v1/auth/telegram/status"),
            Case({ api.unlinkTelegram() }, 204, null, "DELETE", "/v1/auth/telegram"),
            Case({ api.task("t_1") }, 200, Samples.TASK_RUNNING, "GET", "/v1/tasks/t_1"),
            Case({ api.video("tiktok:1") }, 200, Samples.RESOLVE_200, "GET", "/v1/videos/tiktok:1"),
            Case({ api.prepare("tiktok:1") }, 202, Samples.TASK_ID, "POST", "/v1/videos/tiktok:1/prepare"),
            Case({ api.markDownloaded("tiktok:1") }, 204, null, "POST", "/v1/videos/tiktok:1/downloaded"),
            Case({ api.targets() }, 200, Samples.TARGETS, "GET", "/v1/targets?include_hidden=false"),
            Case({ api.targets(includeHidden = true) }, 200, Samples.TARGETS, "GET", "/v1/targets?include_hidden=true"),
            Case({ api.patchTarget(43, PatchTargetRequestDto(true)) }, 200, Samples.TARGET_PATCHED, "PATCH", "/v1/targets/43"),
            Case({ api.refreshTargets() }, 202, null, "POST", "/v1/targets/refresh"),
            Case({ api.send(1001) }, 200, Samples.SEND_SENT, "GET", "/v1/sends/1001"),
            Case({ api.history() }, 200, Samples.HISTORY, "GET", "/v1/history"),
            Case({ api.history("c_4", 20) }, 200, Samples.HISTORY_LAST, "GET", "/v1/history?cursor=c_4&limit=20"),
            Case({ api.appVersion() }, 200, Samples.APP_VERSION, "GET", "/v1/app/version"),
            Case({ api.adminStatus() }, 200, Samples.ADMIN_STATUS, "GET", "/v1/admin/status"),
            Case({ api.invites() }, 200, Samples.INVITES, "GET", "/v1/admin/invites"),
            Case({ api.createInvite(CreateInviteRequestDto("admin", 3)) }, 201, Samples.INVITE_CREATED, "POST", "/v1/admin/invites"),
            Case({ api.revokeInvite(3) }, 204, null, "DELETE", "/v1/admin/invites/3"),
            Case({ api.users() }, 200, Samples.USERS, "GET", "/v1/admin/users"),
            Case({ api.blockUser(8) }, 204, null, "POST", "/v1/admin/users/8/block"),
            Case({ api.unblockUser(8) }, 204, null, "POST", "/v1/admin/users/8/unblock"),
            Case({ api.workers() }, 200, Samples.WORKERS, "GET", "/v1/admin/workers"),
            Case({ api.allowWorker(AllowWorkerRequestDto("nl-1", "cafe")) }, 201, null, "POST", "/v1/admin/workers"),
            Case({ api.revokeWorker("nl-1") }, 204, null, "DELETE", "/v1/admin/workers/nl-1"),
        )
        for (c in cases) {
            enqueue(c.code, c.body)
            c.call()
            val req = server.takeRequest()
            assertEquals(c.path, "${c.method} ${c.path}", "${req.method} ${req.path}")
        }
    }

    @Test
    fun instagramCookiesAreTextPlain() = runTest {
        enqueue(204)
        api.putInstagramCookies(textPlain("# Netscape HTTP Cookie File\n"))
        val req = server.takeRequest()
        assertEquals("PUT", req.method)
        assertEquals("/v1/admin/instagram-cookies", req.path)
        assertTrue(req.getHeader("Content-Type")!!.startsWith("text/plain"))
        assertEquals("# Netscape HTTP Cookie File\n", req.body.readUtf8())
    }

    @Test
    fun absoluteUrls() {
        assertEquals("https://x.example/a.apk", urls.absolute("https://x.example/a.apk"))
        val prefixed = ApiUrls(AppInfo("https://api.example.ru/base", 1, "1"))
        assertEquals("https://api.example.ru/base/v1/videos/a/thumbnail", prefixed.absolute("/v1/videos/a/thumbnail"))
        assertEquals("https://api.example.ru/base/v1/videos/tiktok:1/file", prefixed.file("tiktok:1"))
    }

    private companion object {
        const val DEVICE_ID = "3f0c2b9e-8a41-4c7d-9b1e-2a6f5d4c3b21"
    }
}
