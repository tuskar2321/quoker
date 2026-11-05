package app.quoker.core.data.repository

import androidx.datastore.preferences.core.PreferenceDataStoreFactory
import app.cash.turbine.test
import app.quoker.core.data.FakeClock
import app.quoker.core.data.FakeSessionStore
import app.quoker.core.data.TestApi
import app.quoker.core.data.Wire
import app.quoker.core.data.model.Role
import app.quoker.core.data.model.TelegramLinkState
import app.quoker.core.data.model.UserStatus
import app.quoker.core.data.model.WorkerStatus
import java.io.File
import java.nio.file.Files
import java.time.Duration
import java.time.Instant
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.SocketPolicy
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class AuthAdminVersionRepositoryTest {
    private val t = TestApi(versionCode = 11)
    private val dir: File = Files.createTempDirectory("prefs").toFile()

    @After
    fun tearDown() {
        t.close()
        dir.deleteRecursively()
    }

    @Test
    fun registerStoresSession() = runTest {
        val session = FakeSessionStore(token = null)
        val auth = AuthRepositoryImpl(t.api, session)
        t.enqueue(201, """{"session_token":"new-token","user":${Wire.ME}}""")
        val me = auth.register(" k7q2-zx9p ", "Pixel 9 ")
        assertEquals(Role.USER, me.role)
        assertFalse(me.telegram.linked)
        assertEquals("new-token", session.sessionToken())
        assertTrue(auth.signedIn.first())
        assertEquals(
            """{"invite_code":"K7Q2-ZX9P","device_id":"${FakeSessionStore.DEVICE_ID}","device_name":"Pixel 9"}""",
            t.server.takeRequest().body.readUtf8(),
        )
    }

    @Test
    fun logoutClearsEvenOffline() = runTest {
        val session = FakeSessionStore()
        val auth = AuthRepositoryImpl(t.api, session)
        t.server.enqueue(MockResponse().setSocketPolicy(SocketPolicy.DISCONNECT_AT_START))
        auth.logout()
        assertNull(session.sessionToken())
        assertEquals(1, session.cleared)
    }

    @Test
    fun telegramLinkAndPolling() = runTest {
        val auth = AuthRepositoryImpl(t.api, FakeSessionStore())
        t.enqueue(200, """{"deep_link":"https://t.me/bot?start=link_x","expires_at":"2026-11-03T12:10:00Z"}""")
        val link = auth.createTelegramLink()
        assertEquals(Instant.parse("2026-11-03T12:10:00Z"), link.expiresAt)

        t.enqueue(200, """{"status":"pending"}""")
        t.enqueue(200, """{"status":"pending"}""")
        t.enqueue(200, """{"status":"linked","username":"max","first_name":"Max"}""")
        auth.pollTelegramStatus().test {
            assertEquals(TelegramLinkState.PENDING, awaitItem().state)
            val linked = awaitItem()
            assertEquals(TelegramLinkState.LINKED, linked.state)
            assertEquals("max", linked.username)
            awaitComplete()
        }
    }

    @Test
    fun admin() = runTest {
        val admin = AdminRepositoryImpl(t.api)
        t.enqueue(
            200,
            """{"workers":[{"id":"ru-1","fingerprint":"ab","status":"allowed","connected":true,"version":"1","ytdlp_version":null,"cert_not_after":"2027-11-12T00:00:00Z","last_seen_at":"2026-11-03T11:00:00Z","is_poller":true}],"queue":{"queued":3,"running":1},"errors_24h":{"tiktok":1,"instagram":2,"telegram":0},"disk":{"used_bytes":900,"total_bytes":1000,"cache_bytes":5},"versions":{"core":"v1","app_latest":""}}""",
        )
        val st = admin.status()
        assertEquals(WorkerStatus.ALLOWED, st.workers.single().status)
        assertEquals(java.time.Instant.parse("2027-11-12T00:00:00Z"), st.workers.single().certNotAfter)
        assertEquals(2, st.errors24h.instagram)
        assertEquals(0.9f, st.disk.usedFraction, 0.001f)
        assertNull(st.appLatestVersion)
        assertEquals("v1", st.coreVersion)

        t.enqueue(201, """{"code":"K7Q2-ZX9P","expires_at":"2026-11-10T12:00:00Z"}""")
        assertEquals("K7Q2-ZX9P", admin.createInvite(Role.ADMIN, 3).code)
        t.server.takeRequest()
        assertEquals("""{"role":"admin","expires_in_days":3}""", t.server.takeRequest().body.readUtf8())

        t.enqueue(
            200,
            """{"items":[{"id":8,"role":"user","status":"blocked","telegram_username":null,"telegram_first_name":null,"targets_count":0,"created_at":"2026-10-02T10:00:00Z","last_seen_at":null}]}""",
        )
        assertEquals(UserStatus.BLOCKED, admin.users().single().status)

        t.enqueue(204)
        admin.uploadInstagramCookies("# Netscape HTTP Cookie File\n")
        t.server.takeRequest() // users
        val put = t.server.takeRequest()
        assertEquals("PUT", put.method)
        assertTrue(put.getHeader("Content-Type")!!.startsWith("text/plain"))
    }

    @Test
    fun appVersionCheckedAtMostDaily() = runTest {
        val clock = FakeClock()
        val prefs = PreferenceDataStoreFactory.create(scope = backgroundScope) { File(dir, "app.preferences_pb") }
        val repo = AppVersionRepositoryImpl(t.api, t.appInfo, clock, prefs)
        val body = """{"version_code":12,"version_name":"1.2.0","apk_url":"https://x/download/quoker-1.2.0.apk","sha256":"ab","min_supported_version_code":12,"changelog":"c"}"""

        t.enqueue(200, body)
        val first = repo.checkDaily()!!
        assertTrue(first.updateAvailable)
        assertTrue(first.upgradeRequired)

        clock.now = clock.now.plus(Duration.ofHours(23))
        assertNull(repo.checkDaily())
        assertEquals(1, t.server.requestCount)

        clock.now = clock.now.plus(Duration.ofHours(2))
        t.enqueue(404, Wire.error("not_found"))
        assertNull(repo.checkDaily())
        assertEquals(2, t.server.requestCount)

        t.enqueue(200, body.replace("\"min_supported_version_code\":12", "\"min_supported_version_code\":1"))
        val forced = repo.check()!!
        assertFalse(forced.upgradeRequired)
    }
}
