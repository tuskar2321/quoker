package app.quoker.core.data.model

import app.quoker.core.common.QuokerError
import java.time.Instant

// Domain models used by features; wire DTOs stay in core:network. Unknown enum values from a
// newer core map to UNKNOWN (or the safest value) instead of failing.

enum class Role { USER, ADMIN }

data class Me(
    val id: Long,
    val role: Role,
    val telegram: TelegramAccount,
) {
    val isAdmin: Boolean get() = role == Role.ADMIN
}

data class TelegramAccount(
    val linked: Boolean,
    val username: String?,
    val firstName: String?,
)

data class TelegramLink(
    /** `https://t.me/<bot>?start=link_<token>`. */
    val deepLink: String,
    val expiresAt: Instant?,
)

enum class TelegramLinkState {
    PENDING, LINKED, EXPIRED;

    val isTerminal: Boolean get() = this != PENDING
}

data class TelegramLinkStatus(
    val state: TelegramLinkState,
    val username: String?,
    val firstName: String?,
)

// Videos

enum class Platform { TIKTOK, INSTAGRAM, UNKNOWN }

enum class FileStatus { NONE, PREPARING, READY, FAILED }

data class FileState(
    val status: FileStatus,
    /** Known only when [status] is READY. */
    val sizeBytes: Long?,
)

data class Video(
    /** e.g. `tiktok:7301234567890123456`. */
    val id: String,
    val platform: Platform,
    val sourceUrl: String,
    val canonicalUrl: String,
    val author: String?,
    val caption: String?,
    val durationSeconds: Long?,
    /** Best format size; null when unknown. */
    val sizeBytes: Long?,
    val width: Int?,
    val height: Int?,
    /** TikTok only; null for Instagram. */
    val noWatermark: Boolean?,
    /** Absolute URL; load it with the media OkHttp client (needs the session). Null: no cover. */
    val thumbnailUrl: String?,
    val file: FileState,
)

sealed interface ResolveResult {
    data class Ready(val video: Video) : ResolveResult

    /** The core did not finish within its wait; poll [taskId]. */
    data class Pending(val taskId: String) : ResolveResult
}

sealed interface PrepareResult {
    /** File is in the core cache; GET /file now. */
    data class Ready(val sizeBytes: Long?) : PrepareResult

    data class Pending(val taskId: String) : PrepareResult
}

enum class TaskKind { RESOLVE, DOWNLOAD, UNKNOWN }

enum class TaskStatus {
    QUEUED, RUNNING, DONE, FAILED;

    val isTerminal: Boolean get() = this == DONE || this == FAILED
}

data class Task(
    val id: String,
    val kind: TaskKind,
    val status: TaskStatus,
    /** 0..100, null when unknown. */
    val progress: Int?,
    /** Set when DONE. */
    val video: Video?,
    /** Set when FAILED. */
    val error: QuokerError?,
)

// Targets and sends

enum class TargetType { GROUP, CHANNEL, PRIVATE, UNKNOWN }

enum class TargetStatus { ACTIVE, INACTIVE }

enum class InactiveReason { BOT_REMOVED, BOT_NOT_ADMIN, USER_LEFT, USER_NOT_ADMIN, UNKNOWN }

data class Target(
    val id: Long,
    val title: String,
    val type: TargetType,
    val status: TargetStatus,
    val inactiveReason: InactiveReason?,
    val hidden: Boolean,
    val lastUsedAt: Instant?,
) {
    val isActive: Boolean get() = status == TargetStatus.ACTIVE
}

enum class SendStatus {
    QUEUED, SENDING, SENT, FAILED;

    val isTerminal: Boolean get() = this == SENT || this == FAILED
}

data class Send(
    val id: Long,
    val videoId: String,
    val targetId: Long,
    val status: SendStatus,
    val error: QuokerError?,
    val createdAt: Instant?,
    val updatedAt: Instant?,
)

// History

enum class HistoryActionType { RESOLVED, DOWNLOADED, SENT, SEND_FAILED, UNKNOWN }

data class HistoryAction(
    val type: HistoryActionType,
    val targetTitle: String?,
    /** For "Повторить": GET /v1/sends/{id} gives the target. */
    val sendId: Long?,
    val at: Instant?,
)

data class HistoryItem(
    val id: Long,
    val video: Video,
    /** Newest first. */
    val actions: List<HistoryAction>,
)

data class HistoryPage(
    val items: List<HistoryItem>,
    /** Null on the last page. */
    val nextCursor: String?,
)

// App

data class AppVersion(
    val versionCode: Int,
    val versionName: String,
    val apkUrl: String,
    val sha256: String,
    val minSupportedVersionCode: Int,
    val changelog: String,
)

data class AppVersionCheck(
    val latest: AppVersion,
    /** latest.versionCode > installed versionCode. */
    val updateAvailable: Boolean,
    /** Installed versionCode < latest.minSupportedVersionCode. */
    val upgradeRequired: Boolean,
)

// Admin

enum class WorkerStatus { ALLOWED, REVOKED }

data class Worker(
    val id: String,
    val fingerprint: String,
    val status: WorkerStatus,
    val connected: Boolean,
    val version: String?,
    val ytdlpVersion: String?,
    val lastSeenAt: Instant?,
    val isPoller: Boolean,
)

data class AdminStatus(
    val workers: List<Worker>,
    val queued: Int,
    val running: Int,
    val errors24h: PlatformErrors,
    val disk: DiskUsage,
    val coreVersion: String?,
    /** Null when no release is published. */
    val appLatestVersion: String?,
)

data class PlatformErrors(val tiktok: Int, val instagram: Int, val telegram: Int)

data class DiskUsage(val usedBytes: Long, val totalBytes: Long, val cacheBytes: Long) {
    /** 0..1, 0 when the total is unknown. */
    val usedFraction: Float get() = if (totalBytes > 0) usedBytes.toFloat() / totalBytes else 0f
}

data class Invite(
    val id: Long,
    val role: Role,
    /** First 4 characters, for the mask «K7Q2-••••». */
    val codePrefix: String,
    val createdAt: Instant?,
    val expiresAt: Instant?,
    val usedBy: Long?,
    val usedAt: Instant?,
) {
    val isUsed: Boolean get() = usedBy != null
}

data class CreatedInvite(
    /** `XXXX-XXXX`; shown once. */
    val code: String,
    val expiresAt: Instant?,
)

enum class UserStatus { ACTIVE, BLOCKED }

data class AdminUser(
    val id: Long,
    val role: Role,
    val status: UserStatus,
    val telegramUsername: String?,
    val telegramFirstName: String?,
    val targetsCount: Int,
    val createdAt: Instant?,
    val lastSeenAt: Instant?,
)
