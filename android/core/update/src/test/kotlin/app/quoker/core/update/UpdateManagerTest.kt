package app.quoker.core.update

import android.app.Activity
import android.content.pm.PackageInstaller
import app.quoker.core.common.AppInfo
import app.quoker.core.common.QuokerError
import app.quoker.core.network.ApiUrls
import app.quoker.core.network.ErrorCodes
import app.quoker.core.network.QuokerException
import app.quoker.core.network.SessionEvent
import app.quoker.core.network.SessionEvents
import java.io.File
import java.nio.file.Files
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestScope
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import okhttp3.OkHttpClient
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okio.Buffer
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Robolectric
import org.robolectric.RobolectricTestRunner

@RunWith(RobolectricTestRunner::class)
class UpdateManagerTest {
    private val server = MockWebServer().apply { start() }
    private val dir: File = Files.createTempDirectory("updates").toFile()
    private val repo = FakeAppVersionRepository()
    private val installer = FakeInstaller()
    private val sessionEvents = SessionEvents()

    @After
    fun tearDown() {
        server.shutdown()
        dir.deleteRecursively()
    }

    private fun TestScope.manager(scope: CoroutineScope = backgroundScope) = UpdateManager(
        repository = repo,
        downloader = ApkDownloader(
            OkHttpClient(),
            ApiUrls(AppInfo(server.url("/").toString(), INSTALLED_VERSION, "1.10")),
            dir,
            StandardTestDispatcher(testScheduler),
        ),
        installer = installer,
        sessionEvents = sessionEvents,
        scope = scope,
    )

    @Test
    fun `start check is throttled to once a day`() = runTest {
        repo.latest = release(versionCode = INSTALLED_VERSION)
        manager().apply { checkOnStart() }
        runCurrent()
        assertEquals(1, repo.checkCalls)

        // Next process start, 23 h later: no request.
        repo.nowMs += 23 * 60 * 60 * 1000L
        manager().apply { checkOnStart() }
        runCurrent()
        assertEquals(1, repo.checkCalls)

        // 25 h after the first check: requested again.
        repo.nowMs += 2 * 60 * 60 * 1000L
        manager().apply { checkOnStart() }
        runCurrent()
        assertEquals(2, repo.checkCalls)
    }

    @Test
    fun `checkOnStart runs once per process and uses the daily check`() = runTest {
        val m = manager()
        m.checkOnStart()
        m.checkOnStart()
        runCurrent()
        assertEquals(1, repo.dailyCalls)
    }

    @Test
    fun `newer version is Available, below minimum is Required, same is None`() = runTest {
        data class Case(val versionCode: Int, val minSupported: Int, val expected: (UpdateState) -> Boolean)
        val cases = listOf(
            Case(11, 1) { it is UpdateState.Available && it.info.versionName == "1.11" },
            Case(12, 11) { it is UpdateState.Required && it.info?.versionCode == 12 },
            Case(INSTALLED_VERSION, 1) { it == UpdateState.None },
        )
        for (case in cases) {
            repo.latest = release(case.versionCode, case.minSupported)
            repo.lastCheckAtMs = null
            val m = manager()
            m.checkOnStart()
            runCurrent()
            assertTrue("case $case gave ${m.state.value}", case.expected(m.state.value))
        }
    }

    @Test
    fun `start check errors are silent`() = runTest {
        repo.error = QuokerException(QuokerError(ErrorCodes.NETWORK, "offline", true))
        val m = manager()
        m.checkOnStart()
        runCurrent()
        assertEquals(UpdateState.None, m.state.value)
    }

    @Test
    fun `checkNow ignores the daily limit`() = runTest {
        repo.latest = release(versionCode = INSTALLED_VERSION)
        repo.lastCheckAtMs = repo.nowMs
        val m = manager()
        assertEquals(UpdateCheckResult.UpToDate, m.checkNow())

        repo.latest = release(versionCode = 11)
        val found = m.checkNow()
        assertTrue(found is UpdateCheckResult.Found && !found.required)
        assertTrue(m.state.value is UpdateState.Available)
        assertEquals(2, repo.checkCalls)

        repo.error = QuokerException(QuokerError(ErrorCodes.NETWORK, "offline", true))
        assertEquals(UpdateCheckResult.Failed(repo.error!!.error), m.checkNow())
    }

    @Test
    fun `later dismisses an optional update but not a required one`() = runTest {
        repo.latest = release(versionCode = 11)
        val m = manager()
        m.checkNow()
        m.dismiss()
        assertEquals(UpdateState.None, m.state.value)

        repo.latest = release(versionCode = 12, minSupported = 11)
        m.checkNow()
        m.dismiss()
        assertTrue(m.state.value is UpdateState.Required)
    }

    @Test
    fun `download then install through the installer`() = runTest {
        val bytes = ByteArray(10_000) { it.toByte() }
        server.enqueue(MockResponse().setBody(Buffer().write(bytes)))
        repo.latest = release(versionCode = 11, apkUrl = "/download/quoker-1.11.apk", sha256 = sha256Hex(bytes))
        val m = manager()
        m.checkNow()

        m.download()
        runCurrent()
        val ready = m.state.value
        assertTrue("got $ready", ready is UpdateState.ReadyToInstall && !ready.required)
        assertEquals("/download/quoker-1.11.apk", server.takeRequest().path)

        val activity = Robolectric.buildActivity(Activity::class.java).get()
        installer.allowed = false
        m.proceed(activity)
        runCurrent()
        assertEquals(1, installer.permissionRequests)
        assertTrue(installer.installed.isEmpty())

        installer.allowed = true
        m.proceed(activity)
        runCurrent()
        assertEquals(1, installer.installed.size)

        m.onInstallStatus(PackageInstaller.STATUS_FAILURE_ABORTED, null)
        assertTrue(m.state.value is UpdateState.ReadyToInstall)
        m.onInstallStatus(PackageInstaller.STATUS_FAILURE_INVALID, "bad apk")
        assertEquals(UpdateErrorCodes.INSTALL_FAILED, (m.state.value as UpdateState.Failed).error.code)
    }

    @Test
    fun `checksum mismatch fails the download and keeps no file`() = runTest {
        server.enqueue(MockResponse().setBody("not the apk"))
        repo.latest = release(versionCode = 12, minSupported = 11, sha256 = sha256Hex(byteArrayOf(9)))
        val m = manager()
        m.checkNow()

        m.download()
        runCurrent()

        val failed = m.state.value as UpdateState.Failed
        assertEquals(UpdateErrorCodes.CHECKSUM_MISMATCH, failed.error.code)
        assertTrue("stays blocking", failed.required)
        assertFalse(dir.listFiles().orEmpty().any())
    }

    @Test
    fun `426 session event shows Required and fetches the version`() = runTest {
        repo.latest = release(versionCode = 12, minSupported = 11)
        val m = manager()
        runCurrent()

        sessionEvents.emit(SessionEvent.UpgradeRequired)
        runCurrent()

        val state = m.state.value
        assertTrue("got $state", state is UpdateState.Required && state.info?.versionCode == 12)
    }
}
