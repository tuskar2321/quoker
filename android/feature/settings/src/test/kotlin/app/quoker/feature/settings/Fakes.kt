package app.quoker.feature.settings

import app.quoker.core.common.QuokerError
import app.quoker.core.data.model.AdminStatus
import app.quoker.core.data.model.AdminUser
import app.quoker.core.data.model.CreatedInvite
import app.quoker.core.data.model.DiskUsage
import app.quoker.core.data.model.InactiveReason
import app.quoker.core.data.model.Invite
import app.quoker.core.data.model.Me
import app.quoker.core.data.model.PlatformErrors
import app.quoker.core.data.model.Role
import app.quoker.core.data.model.Target
import app.quoker.core.data.model.TargetStatus
import app.quoker.core.data.model.TargetType
import app.quoker.core.data.model.TelegramAccount
import app.quoker.core.data.model.TelegramLink
import app.quoker.core.data.model.TelegramLinkStatus
import app.quoker.core.data.model.UserStatus
import app.quoker.core.data.model.Worker
import app.quoker.core.data.model.WorkerStatus
import app.quoker.core.data.repository.AdminRepository
import app.quoker.core.data.repository.AuthRepository
import app.quoker.core.data.repository.TargetsRepository
import app.quoker.core.network.QuokerException
import app.quoker.feature.settings.data.CookiesFile
import app.quoker.feature.settings.data.CookiesFileReader
import app.quoker.feature.settings.data.CookiesUploadStore
import java.time.Instant
import kotlin.time.Duration
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.emptyFlow
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.test.StandardTestDispatcher
import kotlinx.coroutines.test.TestDispatcher
import kotlinx.coroutines.test.resetMain
import kotlinx.coroutines.test.setMain
import org.junit.rules.TestWatcher
import org.junit.runner.Description

fun quokerException(code: String) = QuokerException(QuokerError(code, "server text", retryable = true))

@OptIn(ExperimentalCoroutinesApi::class)
class MainDispatcherRule(val dispatcher: TestDispatcher = StandardTestDispatcher()) : TestWatcher() {
    override fun starting(description: Description) = Dispatchers.setMain(dispatcher)

    override fun finished(description: Description) = Dispatchers.resetMain()
}

fun me(id: Long = 1, admin: Boolean = false, linked: Boolean = true) = Me(
    id = id,
    role = if (admin) Role.ADMIN else Role.USER,
    telegram = if (linked) TelegramAccount(true, "max", "Max") else TelegramAccount(false, null, null),
)

fun target(id: Long, hidden: Boolean = false, reason: InactiveReason? = null, type: TargetType = TargetType.GROUP) = Target(
    id = id,
    title = "Target $id",
    type = type,
    status = if (reason == null) TargetStatus.ACTIVE else TargetStatus.INACTIVE,
    inactiveReason = reason,
    hidden = hidden,
    lastUsedAt = null,
)

class FakeAuthRepository : AuthRepository {
    var me: Me = app.quoker.feature.settings.me()
    var meError: QuokerException? = null
    var meCalls = 0
    var unlinkError: QuokerException? = null
    var unlinkCalls = 0
    var logoutCalls = 0

    override val signedIn: Flow<Boolean> = MutableStateFlow(true)

    override suspend fun register(inviteCode: String, deviceName: String): Me = error("unused")

    override suspend fun logout() {
        logoutCalls++
    }

    override suspend fun me(): Me {
        meCalls++
        meError?.let { throw it }
        return me
    }

    override suspend fun createTelegramLink(): TelegramLink = error("unused")

    override suspend fun telegramStatus(): TelegramLinkStatus = error("unused")

    override fun pollTelegramStatus(interval: Duration): Flow<TelegramLinkStatus> = emptyFlow()

    override suspend fun unlinkTelegram() {
        unlinkCalls++
        unlinkError?.let { throw it }
        me = me.copy(telegram = TelegramAccount(false, null, null))
    }
}

/** Mirrors the real repository: [observe] reads the cache that [fetch] and [setHidden] write. */
class FakeTargetsRepository : TargetsRepository {
    val cache = MutableStateFlow<List<Target>>(emptyList())
    var server: List<Target> = emptyList()
    var fetchError: QuokerException? = null
    var fetchCalls = 0
    var refreshError: QuokerException? = null
    var refreshCalls = 0
    var setHiddenError: QuokerException? = null
    var setHiddenGate: CompletableDeferred<Unit>? = null
    val setHiddenCalls = mutableListOf<Pair<Long, Boolean>>()

    override fun observe(includeHidden: Boolean): Flow<List<Target>> =
        cache.map { list -> list.filter { includeHidden || !it.hidden } }

    override suspend fun fetch(): List<Target> {
        fetchCalls++
        fetchError?.let { throw it }
        cache.value = server
        return server
    }

    override suspend fun setHidden(targetId: Long, hidden: Boolean): Target {
        setHiddenCalls += targetId to hidden
        setHiddenGate?.await()
        setHiddenError?.let { throw it }
        val updated = cache.value.first { it.id == targetId }.copy(hidden = hidden)
        server = server.map { if (it.id == targetId) updated else it }
        cache.value = cache.value.map { if (it.id == targetId) updated else it }
        return updated
    }

    override suspend fun refresh() {
        refreshCalls++
        refreshError?.let { throw it }
    }
}

fun worker(id: String, connected: Boolean = true, poller: Boolean = false, status: WorkerStatus = WorkerStatus.ALLOWED) = Worker(
    id = id,
    fingerprint = "ab".repeat(32),
    status = status,
    connected = connected,
    version = "1.0.0",
    ytdlpVersion = "2026.10.22",
    lastSeenAt = null,
    isPoller = poller,
)

fun adminStatus(
    workers: List<Worker> = listOf(worker("worker-1", poller = true)),
    instagramErrors: Int = 0,
    usedBytes: Long = 41,
    totalBytes: Long = 100,
) = AdminStatus(
    workers = workers,
    queued = 1,
    running = 2,
    errors24h = PlatformErrors(tiktok = 0, instagram = instagramErrors, telegram = 0),
    disk = DiskUsage(usedBytes = usedBytes, totalBytes = totalBytes, cacheBytes = 10),
    coreVersion = "1.0.0",
    appLatestVersion = null,
)

fun user(id: Long, status: UserStatus = UserStatus.ACTIVE, username: String? = "user$id", role: Role = Role.USER) =
    AdminUser(id, role, status, username, null, 1, null, null)

class FakeAdminRepository : AdminRepository {
    var status: AdminStatus = adminStatus()
    var statusError: QuokerException? = null
    var invites: MutableList<Invite> = mutableListOf()
    var users: MutableList<AdminUser> = mutableListOf()
    var workers: MutableList<Worker> = mutableListOf()
    var actionError: QuokerException? = null
    val created = mutableListOf<Pair<Role, Int>>()
    val revokedInvites = mutableListOf<Long>()
    val blocked = mutableListOf<Long>()
    val unblocked = mutableListOf<Long>()
    val allowed = mutableListOf<Pair<String, String>>()
    val revokedWorkers = mutableListOf<String>()
    val uploadedCookies = mutableListOf<String>()
    var nextCode = "K7Q2-M4XP"
    var nextExpiry: Instant? = Instant.parse("2026-11-09T12:00:00Z")

    override suspend fun status(): AdminStatus {
        statusError?.let { throw it }
        return status
    }

    override suspend fun invites(): List<Invite> = invites.toList()

    override suspend fun createInvite(role: Role, expiresInDays: Int): CreatedInvite {
        actionError?.let { throw it }
        created += role to expiresInDays
        invites.add(0, Invite(100L + created.size, role, nextCode.take(4), null, nextExpiry, null, null))
        return CreatedInvite(nextCode, nextExpiry)
    }

    override suspend fun revokeInvite(inviteId: Long) {
        actionError?.let { throw it }
        revokedInvites += inviteId
        invites.removeAll { it.id == inviteId }
    }

    override suspend fun users(): List<AdminUser> = users.toList()

    override suspend fun blockUser(userId: Long) {
        actionError?.let { throw it }
        blocked += userId
    }

    override suspend fun unblockUser(userId: Long) {
        actionError?.let { throw it }
        unblocked += userId
    }

    override suspend fun workers(): List<Worker> = workers.toList()

    override suspend fun allowWorker(workerId: String, fingerprint: String) {
        actionError?.let { throw it }
        allowed += workerId to fingerprint
        workers += worker(workerId, connected = false)
    }

    override suspend fun revokeWorker(workerId: String) {
        actionError?.let { throw it }
        revokedWorkers += workerId
        workers = workers.map { if (it.id == workerId) it.copy(status = WorkerStatus.REVOKED, connected = false) else it }.toMutableList()
    }

    override suspend fun uploadInstagramCookies(cookiesTxt: String) {
        actionError?.let { throw it }
        uploadedCookies += cookiesTxt
    }
}

class FakeCookiesUploadStore(initial: Instant? = null) : CookiesUploadStore {
    private val value = MutableStateFlow(initial)
    override val lastUploadAt: Flow<Instant?> = value

    override suspend fun markUploaded(at: Instant) {
        value.value = at
    }
}

class FakeCookiesFileReader(var result: CookiesFile = CookiesFile.Content("# Netscape HTTP Cookie File")) : CookiesFileReader {
    val reads = mutableListOf<String>()

    override suspend fun read(uri: String): CookiesFile {
        reads += uri
        return result
    }
}
