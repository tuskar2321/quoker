package app.quoker.core.data

import app.quoker.core.common.AppInfo
import app.quoker.core.common.Clock
import app.quoker.core.data.session.SessionStore
import app.quoker.core.data.session.TokenCipher
import app.quoker.core.network.ApiUrls
import app.quoker.core.network.AuthHeaderInterceptor
import app.quoker.core.network.ErrorMappingInterceptor
import app.quoker.core.network.QuokerApi
import app.quoker.core.network.SessionEvents
import app.quoker.core.network.di.NetworkModule
import app.quoker.core.network.di.QuokerJson
import java.time.Instant
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.map
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer

/** Real QuokerApi against a MockWebServer, with the production interceptors. */
class TestApi(versionCode: Int = 42) {
    val server = MockWebServer().apply { start() }
    val appInfo = AppInfo(server.url("/").toString(), versionCode, "1.0")
    val urls = ApiUrls(appInfo)
    val events = SessionEvents()
    val session = FakeSessionStore()
    val api: QuokerApi

    init {
        val headers = AuthHeaderInterceptor(appInfo, urls, session, session)
        val client = NetworkModule.provideOkHttpClient(headers, ErrorMappingInterceptor(QuokerJson, events))
        api = NetworkModule.provideQuokerApi(NetworkModule.provideRetrofit(client, QuokerJson, urls))
    }

    fun enqueue(code: Int, body: String? = null) {
        val r = MockResponse().setResponseCode(code)
        if (body != null) r.setBody(body).setHeader("Content-Type", "application/json")
        server.enqueue(r)
    }

    fun close() = server.shutdown()
}

class FakeSessionStore(token: String? = "tok") : SessionStore {
    private val state = MutableStateFlow(token)
    var cleared = 0

    override val signedIn: Flow<Boolean> = state.map { it != null }

    override suspend fun saveToken(token: String) {
        state.value = token
    }

    override suspend fun clear() {
        cleared++
        state.value = null
    }

    override fun sessionToken(): String? = state.value

    override fun deviceId(): String = DEVICE_ID

    companion object {
        const val DEVICE_ID = "3f0c2b9e-8a41-4c7d-9b1e-2a6f5d4c3b21"
    }
}

/** Reversible, obviously-not-plaintext cipher. */
class FakeTokenCipher : TokenCipher {
    override fun encrypt(plaintext: String): String = "enc:" + plaintext.reversed()

    override fun decrypt(ciphertext: String): String? =
        ciphertext.takeIf { it.startsWith("enc:") }?.removePrefix("enc:")?.reversed()
}

class FakeClock(var now: Instant = Instant.parse("2026-11-03T12:00:00Z")) : Clock {
    override fun now(): Instant = now
}

object Wire {
    const val VIDEO = """{"id":"tiktok:1","platform":"tiktok","source_url":"https://vm.tiktok.com/x/","canonical_url":"https://www.tiktok.com/@a/video/1","author":"a","caption":"c","duration_s":27,"size_bytes":100,"width":1080,"height":1920,"no_watermark":true,"thumbnail_url":"/v1/videos/tiktok:1/thumbnail","file":{"status":"none","size_bytes":null}}"""
    const val VIDEO_BARE = """{"id":"instagram:C1","platform":"instagram","source_url":"https://www.instagram.com/reel/C1/","canonical_url":"https://www.instagram.com/reel/C1/","author":null,"caption":null,"duration_s":null,"size_bytes":null,"width":null,"height":null,"no_watermark":null,"thumbnail_url":null,"file":{"status":"none","size_bytes":null}}"""
    const val ME = """{"id":8,"role":"user","telegram":{"linked":false,"username":null,"first_name":null}}"""

    fun task(status: String, progress: Int? = null, video: String? = null, error: String? = null) =
        """{"id":"t_1","kind":"resolve","status":"$status","progress":$progress,"video":$video,"error":$error}"""

    fun send(status: String, error: String? = null) =
        """{"send":{"id":1001,"video_id":"tiktok:1","target_id":42,"status":"$status","error":$error,"created_at":"2026-11-03T12:00:00Z","updated_at":"2026-11-03T12:00:01Z"}}"""

    fun error(code: String, retryable: Boolean = false) =
        """{"error":{"code":"$code","message":"m","retryable":$retryable}}"""

    fun target(id: Long, title: String, hidden: Boolean = false, status: String = "active", reason: String? = null) =
        """{"id":$id,"title":"$title","type":"group","status":"$status","inactive_reason":${reason?.let { "\"$it\"" }},"hidden":$hidden,"last_used_at":null}"""
}
