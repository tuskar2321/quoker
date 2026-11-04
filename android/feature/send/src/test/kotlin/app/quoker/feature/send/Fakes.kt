package app.quoker.feature.send

import app.quoker.core.common.QuokerError
import app.quoker.core.data.model.FileState
import app.quoker.core.data.model.FileStatus
import app.quoker.core.data.model.Me
import app.quoker.core.data.model.Platform
import app.quoker.core.data.model.PrepareResult
import app.quoker.core.data.model.ResolveResult
import app.quoker.core.data.model.Role
import app.quoker.core.data.model.Send
import app.quoker.core.data.model.SendStatus
import app.quoker.core.data.model.Target
import app.quoker.core.data.model.TargetStatus
import app.quoker.core.data.model.TargetType
import app.quoker.core.data.model.InactiveReason
import app.quoker.core.data.model.Task
import app.quoker.core.data.model.TelegramAccount
import app.quoker.core.data.model.TelegramLink
import app.quoker.core.data.model.TelegramLinkStatus
import app.quoker.core.data.model.Video
import app.quoker.core.data.repository.AuthRepository
import app.quoker.core.data.repository.SendsRepository
import app.quoker.core.data.repository.TargetsRepository
import app.quoker.core.data.repository.VideoRepository
import app.quoker.core.network.QuokerException
import java.time.Instant
import kotlin.time.Duration
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.emptyFlow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.map

fun quokerException(code: String) = QuokerException(QuokerError(code, "server text", retryable = false))

fun target(
    id: Long,
    title: String = "T$id",
    type: TargetType = TargetType.GROUP,
    active: Boolean = true,
    reason: InactiveReason? = null,
    hidden: Boolean = false,
    lastUsedAt: Instant? = null,
) = Target(
    id = id,
    title = title,
    type = type,
    status = if (active) TargetStatus.ACTIVE else TargetStatus.INACTIVE,
    inactiveReason = if (active) null else reason ?: InactiveReason.BOT_REMOVED,
    hidden = hidden,
    lastUsedAt = lastUsedAt,
)

class FakeTargetsRepository : TargetsRepository {
    /** The Room cache. */
    val cache = MutableStateFlow<List<Target>>(emptyList())

    /** What GET /v1/targets returns next. */
    var server: List<Target> = emptyList()
    var fetchError: QuokerException? = null
    var refreshError: QuokerException? = null
    var fetchCalls = 0
    var refreshCalls = 0

    override fun observe(includeHidden: Boolean): Flow<List<Target>> =
        cache.map { list -> list.filter { includeHidden || !it.hidden } }

    override suspend fun fetch(): List<Target> {
        fetchCalls++
        fetchError?.let { throw it }
        cache.value = server
        return server
    }

    override suspend fun setHidden(targetId: Long, hidden: Boolean): Target = error("unused")

    override suspend fun refresh() {
        refreshCalls++
        refreshError?.let { throw it }
    }
}

data class CreateCall(
    val videoId: String,
    val targetId: Long,
    val caption: String?,
    val signAsUser: Boolean,
    val key: String,
)

class FakeSendsRepository : SendsRepository {
    private var keys = 0
    val calls = mutableListOf<CreateCall>()
    /** Errors thrown by the next create calls, in order. */
    val errors = ArrayDeque<QuokerException>()

    override fun newIdempotencyKey(): String = "key-${++keys}"

    override suspend fun create(
        videoId: String,
        targetId: Long,
        caption: String?,
        signAsUser: Boolean,
        idempotencyKey: String,
    ): Send {
        calls += CreateCall(videoId, targetId, caption, signAsUser, idempotencyKey)
        errors.removeFirstOrNull()?.let { throw it }
        return Send(1000L + calls.size, videoId, targetId, SendStatus.QUEUED, null, null, null)
    }

    override suspend fun get(sendId: Long): Send = error("unused")

    override fun poll(sendId: Long, interval: Duration): Flow<Send> = emptyFlow()
}

class FakeAuthRepository(
    var telegram: TelegramAccount = TelegramAccount(linked = true, username = "max", firstName = "Max"),
    var meError: QuokerException? = null,
) : AuthRepository {
    override val signedIn: Flow<Boolean> = flowOf(true)
    override suspend fun register(inviteCode: String, deviceName: String): Me = error("unused")
    override suspend fun logout() = Unit
    override suspend fun me(): Me {
        meError?.let { throw it }
        return Me(1, Role.USER, telegram)
    }
    override suspend fun createTelegramLink(): TelegramLink = error("unused")
    override suspend fun telegramStatus(): TelegramLinkStatus = error("unused")
    override fun pollTelegramStatus(interval: Duration): Flow<TelegramLinkStatus> = emptyFlow()
    override suspend fun unlinkTelegram() = Unit
}

class FakeVideoRepository(var getError: QuokerException? = null) : VideoRepository {
    override suspend fun resolve(url: String): ResolveResult = error("unused")
    override suspend fun task(taskId: String): Task = error("unused")
    override fun pollTask(taskId: String, interval: Duration): Flow<Task> = emptyFlow()
    override suspend fun get(videoId: String): Video {
        getError?.let { throw it }
        return Video(
            id = videoId,
            platform = Platform.TIKTOK,
            sourceUrl = "https://vm.tiktok.com/ZM1",
            canonicalUrl = "https://www.tiktok.com/@a/video/1",
            author = "a",
            caption = null,
            durationSeconds = 27,
            sizeBytes = 1,
            width = null,
            height = null,
            noWatermark = true,
            thumbnailUrl = null,
            file = FileState(FileStatus.NONE, null),
        )
    }
    override suspend fun prepare(videoId: String): PrepareResult = error("unused")
    override suspend fun markDownloaded(videoId: String) = Unit
    override fun thumbnailUrl(videoId: String): String = ""
    override fun fileUrl(videoId: String): String = ""
}
