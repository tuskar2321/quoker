package app.quoker.feature.video

import app.quoker.core.common.QuokerError
import app.quoker.core.data.model.FileState
import app.quoker.core.data.model.FileStatus
import app.quoker.core.data.model.Me
import app.quoker.core.data.model.Platform
import app.quoker.core.data.model.PrepareResult
import app.quoker.core.data.model.ResolveResult
import app.quoker.core.data.model.Role
import app.quoker.core.data.model.Send
import app.quoker.core.data.model.Task
import app.quoker.core.data.model.TelegramAccount
import app.quoker.core.data.model.TelegramLink
import app.quoker.core.data.model.TelegramLinkStatus
import app.quoker.core.data.model.Video
import app.quoker.core.data.repository.AuthRepository
import app.quoker.core.data.repository.SendsRepository
import app.quoker.core.data.repository.VideoRepository
import app.quoker.core.download.DownloadState
import app.quoker.core.download.VideoDownloads
import app.quoker.core.network.QuokerException
import kotlin.time.Duration
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.flow

fun video(id: String = "tiktok:1") = Video(
    id = id, platform = Platform.TIKTOK, sourceUrl = "https://vm.tiktok.com/ZM1/",
    canonicalUrl = "https://www.tiktok.com/@a/video/1", author = "a", caption = "c",
    durationSeconds = 27, sizeBytes = 14_680_064, width = 1080, height = 1920, noWatermark = true,
    thumbnailUrl = null, file = FileState(FileStatus.NONE, null),
)

class FakeVideoRepository : VideoRepository {
    var resolveResult: Any = ResolveResult.Ready(video())
    var tasks: List<Task> = emptyList()
    var getResult: Any = video()
    val resolvedUrls = mutableListOf<String>()

    override suspend fun resolve(url: String): ResolveResult {
        resolvedUrls += url
        (resolveResult as? QuokerError)?.let { throw QuokerException(it) }
        return resolveResult as ResolveResult
    }

    override fun pollTask(taskId: String, interval: Duration): Flow<Task> = flow { tasks.forEach { emit(it) } }

    override suspend fun get(videoId: String): Video {
        (getResult as? QuokerError)?.let { throw QuokerException(it) }
        return getResult as Video
    }

    override suspend fun task(taskId: String): Task = tasks.last()
    override suspend fun prepare(videoId: String): PrepareResult = PrepareResult.Ready(1)
    override suspend fun markDownloaded(videoId: String) = Unit
    override fun thumbnailUrl(videoId: String) = "t"
    override fun fileUrl(videoId: String) = "f"
}

class FakeAuthRepository(var linked: Boolean? = true) : AuthRepository {
    var meCalls = 0

    override val signedIn: Flow<Boolean> = MutableStateFlow(true)

    override suspend fun me(): Me {
        meCalls++
        val l = linked ?: throw QuokerException(QuokerError("network", "", true))
        return Me(1, Role.USER, TelegramAccount(l, "u", null))
    }

    override suspend fun register(inviteCode: String, deviceName: String): Me = throw UnsupportedOperationException()
    override suspend fun logout() = Unit
    override suspend fun createTelegramLink(): TelegramLink = throw UnsupportedOperationException()
    override suspend fun telegramStatus(): TelegramLinkStatus = throw UnsupportedOperationException()
    override fun pollTelegramStatus(interval: Duration): Flow<TelegramLinkStatus> = throw UnsupportedOperationException()
    override suspend fun unlinkTelegram() = Unit
}

class FakeSendsRepository : SendsRepository {
    val polls = mutableMapOf<Long, MutableSharedFlow<Send>>()
    var activePolls = 0

    fun pollOf(id: Long) = polls.getOrPut(id) { MutableSharedFlow(replay = 1) }

    override fun poll(sendId: Long, interval: Duration): Flow<Send> = flow {
        activePolls++
        try {
            pollOf(sendId).collect { emit(it) }
        } finally {
            activePolls--
        }
    }

    override fun newIdempotencyKey() = "k"
    override suspend fun create(videoId: String, targetId: Long, caption: String?, signAsUser: Boolean, idempotencyKey: String): Send =
        throw UnsupportedOperationException()
    override suspend fun get(sendId: Long): Send = throw UnsupportedOperationException()
}

class FakeVideoDownloads : VideoDownloads {
    val states = mutableMapOf<String, MutableStateFlow<DownloadState>>()
    val started = mutableListOf<Pair<String, String?>>()
    val cancelled = mutableListOf<String>()
    var notificationPermissionNeeded = false
    var storagePermissionNeeded = false
    var notificationsAsked = false

    fun stateOf(id: String) = states.getOrPut(id) { MutableStateFlow(DownloadState.Idle) }

    override fun start(videoId: String, title: String?) {
        started += videoId to title
        stateOf(videoId).value = DownloadState.Preparing
    }

    override fun cancel(videoId: String) {
        cancelled += videoId
        stateOf(videoId).value = DownloadState.Idle
    }

    override fun observe(videoId: String): Flow<DownloadState> = stateOf(videoId)
    override fun needsNotificationPermission() = notificationPermissionNeeded && !notificationsAsked
    override fun onNotificationPermissionAsked() {
        notificationsAsked = true
    }
    override fun notificationsAllowed() = true
    override fun needsStoragePermission() = storagePermissionNeeded
}
