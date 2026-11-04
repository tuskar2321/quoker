package app.quoker.core.network.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

// Wire models of api/openapi.yaml. Nullability follows what the Go handlers in
// internal/core/httpapi actually emit (they render zero values as null).

@Serializable
data class ErrorBodyDto(
    @SerialName("error") val error: ErrorDto,
)

@Serializable
data class ErrorDto(
    @SerialName("code") val code: String,
    @SerialName("message") val message: String = "",
    @SerialName("retryable") val retryable: Boolean = false,
)

// Auth

@Serializable
data class RegisterRequestDto(
    @SerialName("invite_code") val inviteCode: String,
    @SerialName("device_id") val deviceId: String,
    @SerialName("device_name") val deviceName: String,
)

@Serializable
data class RegisterResponseDto(
    @SerialName("session_token") val sessionToken: String,
    @SerialName("user") val user: MeDto,
)

@Serializable
data class MeDto(
    @SerialName("id") val id: Long,
    @SerialName("role") val role: String,
    @SerialName("telegram") val telegram: TelegramDto = TelegramDto(),
)

@Serializable
data class TelegramDto(
    @SerialName("linked") val linked: Boolean = false,
    @SerialName("username") val username: String? = null,
    @SerialName("first_name") val firstName: String? = null,
)

@Serializable
data class TelegramLinkDto(
    @SerialName("deep_link") val deepLink: String,
    @SerialName("expires_at") val expiresAt: String,
)

@Serializable
data class TelegramStatusDto(
    @SerialName("status") val status: String,
    @SerialName("username") val username: String? = null,
    // Emitted by the core, not in openapi.yaml.
    @SerialName("first_name") val firstName: String? = null,
)

// Videos and tasks

@Serializable
data class ResolveRequestDto(
    @SerialName("url") val url: String,
)

/** 200 carries [video], 202 carries [taskId]. */
@Serializable
data class ResolveResponseDto(
    @SerialName("video") val video: VideoDto? = null,
    @SerialName("task_id") val taskId: String? = null,
)

@Serializable
data class VideoResponseDto(
    @SerialName("video") val video: VideoDto,
)

/** 200 carries [file], 202 carries [taskId]. */
@Serializable
data class PrepareResponseDto(
    @SerialName("file") val file: FileStateDto? = null,
    @SerialName("task_id") val taskId: String? = null,
)

@Serializable
data class FileStateDto(
    @SerialName("status") val status: String,
    @SerialName("size_bytes") val sizeBytes: Long? = null,
)

@Serializable
data class VideoDto(
    @SerialName("id") val id: String,
    @SerialName("platform") val platform: String,
    @SerialName("source_url") val sourceUrl: String,
    @SerialName("canonical_url") val canonicalUrl: String,
    @SerialName("author") val author: String? = null,
    @SerialName("caption") val caption: String? = null,
    @SerialName("duration_s") val durationS: Long? = null,
    @SerialName("size_bytes") val sizeBytes: Long? = null,
    @SerialName("width") val width: Int? = null,
    @SerialName("height") val height: Int? = null,
    @SerialName("no_watermark") val noWatermark: Boolean? = null,
    @SerialName("thumbnail_url") val thumbnailUrl: String? = null,
    @SerialName("file") val file: FileStateDto = FileStateDto(status = "none"),
)

@Serializable
data class TaskDto(
    @SerialName("id") val id: String,
    @SerialName("kind") val kind: String,
    @SerialName("status") val status: String,
    @SerialName("progress") val progress: Int? = null,
    @SerialName("video") val video: VideoDto? = null,
    @SerialName("error") val error: ErrorDto? = null,
)

// Targets

@Serializable
data class TargetsResponseDto(
    @SerialName("items") val items: List<TargetDto> = emptyList(),
)

@Serializable
data class TargetResponseDto(
    @SerialName("target") val target: TargetDto,
)

@Serializable
data class TargetDto(
    @SerialName("id") val id: Long,
    @SerialName("title") val title: String,
    @SerialName("type") val type: String,
    @SerialName("status") val status: String,
    @SerialName("inactive_reason") val inactiveReason: String? = null,
    @SerialName("hidden") val hidden: Boolean = false,
    @SerialName("last_used_at") val lastUsedAt: String? = null,
)

@Serializable
data class PatchTargetRequestDto(
    @SerialName("hidden") val hidden: Boolean,
)

// Sends

@Serializable
data class CreateSendRequestDto(
    @SerialName("video_id") val videoId: String,
    @SerialName("target_id") val targetId: Long,
    // null: the core adds the original link; "": no caption. Always encoded.
    @SerialName("caption") val caption: String?,
    // The core treats a missing field as false, so it is always sent.
    @SerialName("sign_as_user") val signAsUser: Boolean,
)

@Serializable
data class SendResponseDto(
    @SerialName("send") val send: SendDto,
)

@Serializable
data class SendDto(
    @SerialName("id") val id: Long,
    @SerialName("video_id") val videoId: String,
    @SerialName("target_id") val targetId: Long,
    @SerialName("status") val status: String,
    @SerialName("error") val error: ErrorDto? = null,
    @SerialName("created_at") val createdAt: String? = null,
    @SerialName("updated_at") val updatedAt: String? = null,
)

// History

@Serializable
data class HistoryPageDto(
    @SerialName("items") val items: List<HistoryItemDto> = emptyList(),
    @SerialName("next_cursor") val nextCursor: String? = null,
)

@Serializable
data class HistoryItemDto(
    @SerialName("id") val id: Long,
    @SerialName("video") val video: VideoDto,
    @SerialName("actions") val actions: List<HistoryActionDto> = emptyList(),
)

@Serializable
data class HistoryActionDto(
    @SerialName("type") val type: String,
    @SerialName("target_title") val targetTitle: String? = null,
    @SerialName("send_id") val sendId: Long? = null,
    @SerialName("at") val at: String? = null,
)

// App

@Serializable
data class AppVersionDto(
    @SerialName("version_code") val versionCode: Int,
    @SerialName("version_name") val versionName: String,
    @SerialName("apk_url") val apkUrl: String,
    @SerialName("sha256") val sha256: String,
    @SerialName("min_supported_version_code") val minSupportedVersionCode: Int = 0,
    @SerialName("changelog") val changelog: String = "",
)

// Admin

@Serializable
data class AdminStatusDto(
    @SerialName("workers") val workers: List<WorkerDto> = emptyList(),
    @SerialName("queue") val queue: QueueDto = QueueDto(),
    // Keys: tiktok, instagram, telegram.
    @SerialName("errors_24h") val errors24h: Map<String, Int> = emptyMap(),
    @SerialName("disk") val disk: DiskDto = DiskDto(),
    // Keys: core, app_latest ("" when no release is published).
    @SerialName("versions") val versions: Map<String, String> = emptyMap(),
)

@Serializable
data class QueueDto(
    @SerialName("queued") val queued: Int = 0,
    @SerialName("running") val running: Int = 0,
)

@Serializable
data class DiskDto(
    @SerialName("used_bytes") val usedBytes: Long = 0,
    @SerialName("total_bytes") val totalBytes: Long = 0,
    @SerialName("cache_bytes") val cacheBytes: Long = 0,
)

@Serializable
data class WorkersResponseDto(
    @SerialName("items") val items: List<WorkerDto> = emptyList(),
)

@Serializable
data class WorkerDto(
    @SerialName("id") val id: String,
    @SerialName("fingerprint") val fingerprint: String = "",
    @SerialName("status") val status: String,
    @SerialName("connected") val connected: Boolean = false,
    @SerialName("version") val version: String? = null,
    @SerialName("ytdlp_version") val ytdlpVersion: String? = null,
    @SerialName("last_seen_at") val lastSeenAt: String? = null,
    @SerialName("is_poller") val isPoller: Boolean = false,
)

@Serializable
data class AllowWorkerRequestDto(
    @SerialName("id") val id: String,
    @SerialName("fingerprint") val fingerprint: String,
)

@Serializable
data class InvitesResponseDto(
    @SerialName("items") val items: List<InviteDto> = emptyList(),
)

@Serializable
data class InviteDto(
    @SerialName("id") val id: Long,
    @SerialName("role") val role: String,
    @SerialName("code_prefix") val codePrefix: String,
    @SerialName("created_at") val createdAt: String? = null,
    @SerialName("expires_at") val expiresAt: String? = null,
    @SerialName("used_by") val usedBy: Long? = null,
    @SerialName("used_at") val usedAt: String? = null,
)

@Serializable
data class CreateInviteRequestDto(
    @SerialName("role") val role: String,
    @SerialName("expires_in_days") val expiresInDays: Int,
)

@Serializable
data class CreatedInviteDto(
    @SerialName("code") val code: String,
    @SerialName("expires_at") val expiresAt: String? = null,
)

@Serializable
data class AdminUsersResponseDto(
    @SerialName("items") val items: List<AdminUserDto> = emptyList(),
)

@Serializable
data class AdminUserDto(
    @SerialName("id") val id: Long,
    @SerialName("role") val role: String,
    @SerialName("status") val status: String,
    @SerialName("telegram_username") val telegramUsername: String? = null,
    @SerialName("telegram_first_name") val telegramFirstName: String? = null,
    @SerialName("targets_count") val targetsCount: Int = 0,
    @SerialName("created_at") val createdAt: String? = null,
    @SerialName("last_seen_at") val lastSeenAt: String? = null,
)
