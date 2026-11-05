package app.quoker.core.download

import app.quoker.core.common.QuokerError
import app.quoker.core.data.model.FileState
import app.quoker.core.data.model.FileStatus
import app.quoker.core.data.model.Platform
import app.quoker.core.data.model.PrepareResult
import app.quoker.core.data.model.ResolveResult
import app.quoker.core.data.model.Task
import app.quoker.core.data.model.TaskKind
import app.quoker.core.data.model.TaskStatus
import app.quoker.core.data.model.Video
import app.quoker.core.data.repository.VideoRepository
import app.quoker.core.download.internal.SavedVideos
import app.quoker.core.download.internal.VideoSaver
import app.quoker.core.network.QuokerException
import java.io.File
import java.io.IOException
import kotlin.time.Duration
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.flow
import kotlinx.coroutines.flow.map

class FakeVideoRepository(private val fileUrl: String) : VideoRepository {
    /** Results of successive prepare() calls; the last one repeats. */
    val prepareResults = ArrayDeque<Any>(listOf(PrepareResult.Ready(10)))
    var taskStates: List<Task> = emptyList()
    var prepareCalls = 0
    var markCalls = 0
    var markError: QuokerError? = null

    override suspend fun prepare(videoId: String): PrepareResult {
        prepareCalls++
        val next = if (prepareResults.size > 1) prepareResults.removeFirst() else prepareResults.first()
        if (next is QuokerError) throw QuokerException(next)
        return next as PrepareResult
    }

    override fun pollTask(taskId: String, interval: Duration): Flow<Task> = flow { taskStates.forEach { emit(it) } }

    override suspend fun markDownloaded(videoId: String) {
        markCalls++
        markError?.let { throw QuokerException(it) }
    }

    override fun fileUrl(videoId: String): String = fileUrl

    override fun thumbnailUrl(videoId: String): String = "$fileUrl/thumb"

    override suspend fun resolve(url: String): ResolveResult = throw UnsupportedOperationException()

    override suspend fun task(taskId: String): Task = throw UnsupportedOperationException()

    override suspend fun get(videoId: String): Video = throw UnsupportedOperationException()
}

class FakeVideoSaver : VideoSaver {
    val saved = mutableListOf<Pair<ByteArray, String>>()
    var failure: IOException? = null

    override suspend fun save(source: File, displayName: String): String {
        failure?.let { throw it }
        saved += source.readBytes() to displayName
        return "content://media/external/video/media/${saved.size}"
    }
}

fun task(status: TaskStatus, error: QuokerError? = null) =
    Task("t1", TaskKind.DOWNLOAD, status, progress = null, video = null, error = error)

fun sampleVideo(id: String = "tiktok:1") = Video(
    id = id, platform = Platform.TIKTOK, sourceUrl = "https://vm.tiktok.com/ZM1/",
    canonicalUrl = "https://www.tiktok.com/@a/video/1", author = "a", caption = null,
    durationSeconds = 27, sizeBytes = 10, width = null, height = null, noWatermark = true,
    thumbnailUrl = null, file = FileState(FileStatus.NONE, null),
)

class FakeSavedVideos : SavedVideos {
    val uris = MutableStateFlow<Map<String, String>>(emptyMap())
    /** Uris still present in the gallery. */
    val existing = mutableSetOf<String>()
    var existsCalls = 0

    override suspend fun record(videoId: String, uri: String) {
        uris.value += videoId to uri
        existing += uri
    }

    override fun observe(videoId: String): Flow<String?> = uris.map { it[videoId] }.distinctUntilChanged()

    override suspend fun exists(uri: String): Boolean {
        existsCalls++
        return uri in existing
    }
}
