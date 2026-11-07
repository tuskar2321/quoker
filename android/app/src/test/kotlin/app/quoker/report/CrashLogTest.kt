package app.quoker.report

import java.io.File
import java.nio.file.Files
import java.time.Instant
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(application = android.app.Application::class)
class CrashLogTest {
    private val now = Instant.parse("2026-11-07T10:00:00Z")

    private fun newLog(maxBytes: Int = CrashLog.MAX_BYTES): Pair<CrashLog, File> {
        val dir = Files.createTempDirectory("reports").toFile()
        return CrashLog(dir, "0.2.0 (2)", { now }, maxBytes) to dir
    }

    @Test
    fun reportContainsDeviceInfoAndCrashes() {
        data class Case(val name: String, val crashes: List<String>, val expected: List<String>, val absent: List<String>)
        val cases = listOf(
            Case("no crashes", emptyList(), listOf("App: 0.2.0 (2)", "No crashes recorded."), listOf("===")),
            Case(
                "two crashes",
                listOf("first boom", "second boom"),
                listOf("=== 2026-11-07T10:00:00Z · thread main · 0.2.0 (2)", "IllegalStateException: first boom", "second boom"),
                listOf("No crashes recorded."),
            ),
        )
        for (case in cases) {
            val (log, _) = newLog()
            case.crashes.forEach { log.append("main", IllegalStateException(it)) }
            val text = log.writeReport().readText()
            assertTrue(case.name, text.startsWith("Quoker report\nGenerated: 2026-11-07T10:00:00Z\n"))
            case.expected.forEach { assertTrue("${case.name}: $it", text.contains(it)) }
            case.absent.forEach { assertFalse("${case.name}: $it", text.contains(it)) }
        }
    }

    @Test
    fun logIsTrimmedToNewestEntries() {
        val (log, dir) = newLog(maxBytes = 8 * 1024)
        repeat(50) { log.append("main", RuntimeException("crash-$it")) }
        val text = File(dir, CrashLog.LOG_FILE).readText()
        assertTrue(text.length <= 8 * 1024)
        assertTrue("starts at an entry", text.startsWith("=== "))
        assertTrue("newest kept", text.contains("crash-49"))
        assertFalse("oldest dropped", text.contains("crash-0\n"))
    }

    @Test
    fun installedHandlerRecordsAndDelegates() {
        val (log, dir) = newLog()
        val previous = Thread.getDefaultUncaughtExceptionHandler()
        var delegated: Throwable? = null
        Thread.setDefaultUncaughtExceptionHandler { _, e -> delegated = e }
        try {
            log.install()
            val error = IllegalArgumentException("from worker thread")
            Thread.getDefaultUncaughtExceptionHandler()!!.uncaughtException(Thread.currentThread(), error)
            assertEquals(error, delegated)
            assertTrue(File(dir, CrashLog.LOG_FILE).readText().contains("from worker thread"))
        } finally {
            Thread.setDefaultUncaughtExceptionHandler(previous)
        }
    }
}
