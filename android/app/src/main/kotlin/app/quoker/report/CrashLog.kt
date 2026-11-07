package app.quoker.report

import android.content.ClipData
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.os.Build
import androidx.core.content.FileProvider
import app.quoker.ShareActivity
import java.io.File
import java.io.PrintWriter
import java.io.StringWriter
import java.time.Instant

/**
 * Local crash log (docs/11 «Приватность»): no third-party crash reporting. Uncaught exceptions are
 * appended to files/reports/crash.log; «Отправить отчёт» in Settings shares a report with the
 * device info and that log through the system share sheet, so the user decides whom to send it to.
 */
internal class CrashLog(
    private val dir: File,
    private val appVersion: String,
    private val now: () -> Instant = Instant::now,
    private val maxBytes: Int = MAX_BYTES,
) {
    private val log = File(dir, LOG_FILE)

    /** Records [error] and hands it on to the previous handler (which ends the process). */
    fun install() {
        val previous = Thread.getDefaultUncaughtExceptionHandler()
        Thread.setDefaultUncaughtExceptionHandler { thread, error ->
            runCatching { append(thread.name, error) }
            previous?.uncaughtException(thread, error)
        }
    }

    @Synchronized
    fun append(threadName: String, error: Throwable) {
        dir.mkdirs()
        val trace = StringWriter().also { error.printStackTrace(PrintWriter(it)) }.toString()
        log.appendText("=== ${now()} · thread $threadName · $appVersion\n$trace\n")
        trim()
    }

    /** Writes the report (device info + crash log) and returns it. */
    @Synchronized
    fun writeReport(): File {
        dir.mkdirs()
        val crashes = if (log.exists() && log.length() > 0) log.readText() else "No crashes recorded.\n"
        val report = File(dir, REPORT_FILE)
        report.writeText(
            buildString {
                appendLine("Quoker report")
                appendLine("Generated: ${now()}")
                appendLine("App: $appVersion")
                appendLine("Android: ${Build.VERSION.RELEASE} (API ${Build.VERSION.SDK_INT})")
                appendLine("Device: ${Build.MANUFACTURER} ${Build.MODEL}")
                appendLine()
                append(crashes)
            },
        )
        return report
    }

    // Keeps the newest half when the log outgrows maxBytes, starting at an entry boundary.
    private fun trim() {
        if (log.length() <= maxBytes) return
        val text = log.readText()
        val tail = text.takeLast(maxBytes / 2)
        val start = tail.indexOf(ENTRY_MARK).takeIf { it >= 0 } ?: 0
        log.writeText(tail.substring(start))
    }

    companion object {
        const val DIR = "reports"
        const val LOG_FILE = "crash.log"
        const val REPORT_FILE = "quoker-report.txt"
        const val MAX_BYTES = 256 * 1024
        private const val ENTRY_MARK = "=== "

        fun forApp(context: Context, appVersion: String) =
            CrashLog(File(context.filesDir, DIR), appVersion)

        /** Share-sheet intent with the report attached (FileProvider `<applicationId>.reports`). */
        fun shareIntent(context: Context, report: File, subject: String, chooserTitle: String): Intent {
            val uri = FileProvider.getUriForFile(context, "${context.packageName}.reports", report)
            val send = Intent(Intent.ACTION_SEND)
                .setType("text/plain")
                .putExtra(Intent.EXTRA_STREAM, uri)
                .putExtra(Intent.EXTRA_SUBJECT, subject)
                .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
            send.clipData = ClipData.newRawUri(null, uri)
            // Quoker itself accepts text/plain shares; sending the report to itself makes no sense.
            return Intent.createChooser(send, chooserTitle)
                .putExtra(Intent.EXTRA_EXCLUDE_COMPONENTS, arrayOf(ComponentName(context, ShareActivity::class.java)))
        }
    }
}
