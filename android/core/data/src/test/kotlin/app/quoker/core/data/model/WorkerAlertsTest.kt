package app.quoker.core.data.model

import java.time.Instant
import org.junit.Assert.assertEquals
import org.junit.Test

class WorkerAlertsTest {
    private val now = Instant.parse("2026-11-05T12:00:00Z")

    private fun worker(
        connected: Boolean,
        lastSeenAgoSeconds: Long?,
        status: WorkerStatus = WorkerStatus.ALLOWED,
    ) = Worker(
        id = "w",
        fingerprint = "ab",
        status = status,
        connected = connected,
        version = null,
        ytdlpVersion = null,
        certNotAfter = null,
        lastSeenAt = lastSeenAgoSeconds?.let { now.minusSeconds(it) },
        isPoller = false,
    )

    @Test
    fun noWorkersAlert() {
        val cases = listOf(
            Triple("no workers at all", emptyList<Worker>(), true),
            Triple("only revoked", listOf(worker(connected = true, lastSeenAgoSeconds = 0, status = WorkerStatus.REVOKED)), true),
            Triple("one connected", listOf(worker(false, 3600), worker(true, 0)), false),
            Triple("offline for 4:59", listOf(worker(false, 299)), false),
            Triple("offline for exactly 5 min", listOf(worker(false, 300)), true),
            Triple("offline for an hour", listOf(worker(false, 3600)), true),
            Triple("newest last seen counts", listOf(worker(false, 3600), worker(false, 60)), false),
            Triple("never seen", listOf(worker(false, null)), true),
            Triple("revoked seen recently does not delay", listOf(worker(false, 3600), worker(false, 10, WorkerStatus.REVOKED)), true),
        )
        for ((name, workers, expected) in cases) {
            assertEquals(name, expected, workers.noWorkersAlert(now))
        }
    }
}
