package app.quoker.core.download

import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.work.Data
import androidx.work.WorkInfo
import androidx.work.workDataOf
import app.quoker.core.common.QuokerError
import app.quoker.core.download.DownloadWorker.Companion.errorData
import java.util.UUID
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class DownloadStateMappingTest {
    private fun info(state: WorkInfo.State, output: Data = Data.EMPTY, progress: Data = Data.EMPTY) =
        WorkInfo(UUID.randomUUID(), state, emptySet(), output, progress)

    @Test
    fun mapsWorkInfoToDownloadState() {
        val error = QuokerError("private_video", "m", retryable = false)
        val cases = listOf(
            null to DownloadState.Idle,
            info(WorkInfo.State.CANCELLED) to DownloadState.Idle,
            info(WorkInfo.State.ENQUEUED) to DownloadState.Preparing,
            info(WorkInfo.State.RUNNING) to DownloadState.Preparing,
            info(WorkInfo.State.RUNNING, progress = workDataOf(DownloadWorker.KEY_PHASE to DownloadWorker.PHASE_PREPARING)) to DownloadState.Preparing,
            info(
                WorkInfo.State.RUNNING,
                progress = workDataOf(DownloadWorker.KEY_PHASE to DownloadWorker.PHASE_DOWNLOADING, DownloadWorker.KEY_PERCENT to 62),
            ) to DownloadState.Downloading(62),
            info(WorkInfo.State.SUCCEEDED, output = workDataOf(DownloadWorker.KEY_URI to "content://x/1")) to DownloadState.Saved("content://x/1"),
            info(WorkInfo.State.FAILED, output = errorData(error)) to DownloadState.Failed(error),
        )
        for ((input, expected) in cases) {
            assertEquals("for ${input?.state}", expected, input.toDownloadState())
        }
    }

    @Test
    fun latestPrefersUnfinishedWork() {
        val done = info(WorkInfo.State.SUCCEEDED)
        val running = info(WorkInfo.State.RUNNING)
        assertEquals(running, latest(listOf(done, running)))
        assertEquals(done, latest(listOf(info(WorkInfo.State.FAILED), done)))
        assertEquals(null, latest(emptyList()))
    }
}
