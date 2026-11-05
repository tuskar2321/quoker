package app.quoker.feature.home

import app.quoker.core.common.QuokerError
import app.quoker.core.data.model.AdminStatus
import app.quoker.core.data.model.AdminUser
import app.quoker.core.data.model.CreatedInvite
import app.quoker.core.data.model.DiskUsage
import app.quoker.core.data.model.FileState
import app.quoker.core.data.model.FileStatus
import app.quoker.core.data.model.HistoryAction
import app.quoker.core.data.model.HistoryActionType
import app.quoker.core.data.model.HistoryItem
import app.quoker.core.data.model.HistoryPage
import app.quoker.core.data.model.Invite
import app.quoker.core.data.model.Me
import app.quoker.core.data.model.Platform
import app.quoker.core.data.model.PlatformErrors
import app.quoker.core.data.model.Role
import app.quoker.core.data.model.TelegramAccount
import app.quoker.core.data.model.TelegramLink
import app.quoker.core.data.model.TelegramLinkStatus
import app.quoker.core.data.model.Video
import app.quoker.core.data.model.Worker
import app.quoker.core.data.model.WorkerStatus
import app.quoker.core.data.repository.AdminRepository
import app.quoker.core.data.repository.AuthRepository
import app.quoker.core.data.repository.HistoryRepository
import app.quoker.core.network.QuokerException
import java.time.Instant
import kotlin.time.Duration
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.test.TestDispatcher
import kotlinx.coroutines.test.UnconfinedTestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.setMain
import org.junit.rules.TestWatcher
import org.junit.runner.Description

@OptIn(ExperimentalCoroutinesApi::class)
class MainDispatcherRule(val dispatcher: TestDispatcher = UnconfinedTestDispatcher()) : TestWatcher() {
    override fun starting(description: Description) = Dispatchers.setMain(dispatcher)
    override fun finished(description: Description) = Dispatchers.resetMain()
}

fun networkError() = QuokerException(QuokerError("network", "", true))

fun video(id: String, platform: Platform = Platform.TIKTOK, author: String? = "author") = Video(
    id = id,
    platform = platform,
    sourceUrl = "https://vm.tiktok.com/$id/",
    canonicalUrl = "https://www.tiktok.com/@author/video/$id",
    author = author,
    caption = null,
    durationSeconds = 27,
    sizeBytes = null,
    width = null,
    height = null,
    noWatermark = true,
    thumbnailUrl = "https://core/v1/videos/$id/thumbnail",
    file = FileState(FileStatus.NONE, null),
)

fun action(type: HistoryActionType, target: String? = null, at: Instant? = Instant.parse("2026-11-04T12:00:00Z")) =
    HistoryAction(type, target, sendId = null, at = at)

fun historyItem(id: Long, vararg actions: HistoryAction) = HistoryItem(id, video(id.toString()), actions.toList())

class FakeHistoryRepository : HistoryRepository {
    val page = MutableStateFlow(HistoryPage(emptyList(), null))
    var refreshes = 0
    var refreshFailure: QuokerException? = null

    override val cachedFirstPage: Flow<HistoryPage> = page

    override suspend fun refresh(limit: Int): HistoryPage {
        refreshes++
        refreshFailure?.let { throw it }
        return page.value
    }

    override suspend fun page(cursor: String, limit: Int): HistoryPage = error("not used")
}

class FakeAuthRepository(var role: Role = Role.USER) : AuthRepository {
    var meCalls = 0
    override val signedIn: Flow<Boolean> = MutableStateFlow(true)
    override suspend fun register(inviteCode: String, deviceName: String): Me = error("not used")
    override suspend fun logout() = Unit
    override suspend fun me(): Me {
        meCalls++
        return Me(1, role, TelegramAccount(true, "max", null))
    }
    override suspend fun createTelegramLink(): TelegramLink = error("not used")
    override suspend fun telegramStatus(): TelegramLinkStatus = error("not used")
    override fun pollTelegramStatus(interval: Duration): Flow<TelegramLinkStatus> = error("not used")
    override suspend fun unlinkTelegram() = Unit
}

fun worker(connected: Boolean, status: WorkerStatus = WorkerStatus.ALLOWED, lastSeenAt: java.time.Instant? = null) =
    Worker("w1", "ab", status, connected, null, null, certNotAfter = null, lastSeenAt = lastSeenAt, isPoller = connected)

class FakeAdminRepository : AdminRepository {
    var workers: List<Worker> = emptyList()
    var statusCalls = 0

    override suspend fun status(): AdminStatus {
        statusCalls++
        return AdminStatus(workers, 0, 0, PlatformErrors(0, 0, 0), DiskUsage(0, 0, 0), null, null)
    }

    override suspend fun invites(): List<Invite> = error("not used")
    override suspend fun createInvite(role: Role, expiresInDays: Int): CreatedInvite = error("not used")
    override suspend fun revokeInvite(inviteId: Long) = error("not used")
    override suspend fun users(): List<AdminUser> = error("not used")
    override suspend fun blockUser(userId: Long) = error("not used")
    override suspend fun unblockUser(userId: Long) = error("not used")
    override suspend fun workers(): List<Worker> = error("not used")
    override suspend fun allowWorker(workerId: String, fingerprint: String) = error("not used")
    override suspend fun revokeWorker(workerId: String) = error("not used")
    override suspend fun uploadInstagramCookies(cookiesTxt: String) = error("not used")
}

class FakeNetworkMonitor : NetworkMonitor {
    override val isOnline = MutableStateFlow(true)
}
