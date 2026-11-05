package app.quoker.core.download

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertEquals
import org.junit.Test

class SavedStateTest {
    private data class Case(
        val name: String,
        val work: DownloadState,
        val stored: String?,
        val existing: Set<String>,
        val expected: DownloadState,
    )

    @Test
    fun workStateFallsBackToTheSavedFile() = runTest {
        val cases = listOf(
            Case("nothing", DownloadState.Idle, null, emptySet(), DownloadState.Idle),
            Case("work pruned, file present", DownloadState.Idle, "content://m/1", setOf("content://m/1"), DownloadState.Saved("content://m/1")),
            Case("work pruned, file deleted", DownloadState.Idle, "content://m/1", emptySet(), DownloadState.Idle),
            Case("work saved, file deleted", DownloadState.Saved("content://m/2"), "content://m/2", emptySet(), DownloadState.Idle),
            Case("work saved, file present", DownloadState.Saved("content://m/2"), "content://m/2", setOf("content://m/2"), DownloadState.Saved("content://m/2")),
            Case("re-download running wins", DownloadState.Downloading(40), "content://m/1", setOf("content://m/1"), DownloadState.Downloading(40)),
            Case("re-download preparing wins", DownloadState.Preparing, "content://m/1", setOf("content://m/1"), DownloadState.Preparing),
        )
        for (c in cases) {
            val saved = FakeSavedVideos().apply {
                c.stored?.let { uris.value = mapOf("v" to it) }
                existing += c.existing
            }
            val state = withSavedFile(MutableStateFlow(c.work), saved, "v").first()
            assertEquals(c.name, c.expected, state)
        }
    }
}
