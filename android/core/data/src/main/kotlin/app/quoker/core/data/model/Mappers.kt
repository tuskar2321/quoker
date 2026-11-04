package app.quoker.core.data.model

import app.quoker.core.common.QuokerError
import app.quoker.core.network.ApiUrls
import app.quoker.core.network.model.AdminStatusDto
import app.quoker.core.network.model.AdminUserDto
import app.quoker.core.network.model.AppVersionDto
import app.quoker.core.network.model.CreatedInviteDto
import app.quoker.core.network.model.ErrorDto
import app.quoker.core.network.model.FileStateDto
import app.quoker.core.network.model.HistoryActionDto
import app.quoker.core.network.model.HistoryItemDto
import app.quoker.core.network.model.HistoryPageDto
import app.quoker.core.network.model.InviteDto
import app.quoker.core.network.model.MeDto
import app.quoker.core.network.model.SendDto
import app.quoker.core.network.model.TargetDto
import app.quoker.core.network.model.TaskDto
import app.quoker.core.network.model.TelegramLinkDto
import app.quoker.core.network.model.TelegramStatusDto
import app.quoker.core.network.model.VideoDto
import app.quoker.core.network.model.WorkerDto
import java.time.Instant
import java.time.format.DateTimeParseException

internal fun String?.toInstantOrNull(): Instant? = this?.let {
    try {
        Instant.parse(it)
    } catch (_: DateTimeParseException) {
        null
    }
}

private inline fun <reified E : Enum<E>> String?.toEnum(fallback: E): E =
    this?.let { raw -> enumValues<E>().firstOrNull { it.name.equals(raw, ignoreCase = true) } } ?: fallback

internal fun String.toRole(): Role = toEnum(Role.USER)

internal fun Role.wire(): String = name.lowercase()

internal fun ErrorDto.toDomain() = QuokerError(code, message, retryable)

internal fun MeDto.toDomain() = Me(
    id = id,
    role = role.toRole(),
    telegram = TelegramAccount(telegram.linked, telegram.username, telegram.firstName),
)

internal fun TelegramLinkDto.toDomain() = TelegramLink(deepLink, expiresAt.toInstantOrNull())

internal fun TelegramStatusDto.toDomain() = TelegramLinkStatus(
    // An unknown state is treated as expired: the UI then offers a new link instead of waiting forever.
    state = status.toEnum(TelegramLinkState.EXPIRED),
    username = username,
    firstName = firstName,
)

internal fun FileStateDto.toDomain() = FileState(status.toEnum(FileStatus.NONE), sizeBytes)

internal fun VideoDto.toDomain(urls: ApiUrls) = Video(
    id = id,
    platform = platform.toEnum(Platform.UNKNOWN),
    sourceUrl = sourceUrl,
    canonicalUrl = canonicalUrl,
    author = author,
    caption = caption,
    durationSeconds = durationS,
    sizeBytes = sizeBytes,
    width = width,
    height = height,
    noWatermark = noWatermark,
    thumbnailUrl = thumbnailUrl?.let(urls::absolute),
    file = file.toDomain(),
)

internal fun TaskDto.toDomain(urls: ApiUrls) = Task(
    id = id,
    kind = kind.toEnum(TaskKind.UNKNOWN),
    // Unknown states keep polling rather than ending early.
    status = status.toEnum(TaskStatus.RUNNING),
    progress = progress,
    video = video?.toDomain(urls),
    error = error?.toDomain(),
)

internal fun TargetDto.toDomain() = Target(
    id = id,
    title = title,
    type = type.toEnum(TargetType.UNKNOWN),
    status = status.toEnum(TargetStatus.INACTIVE),
    inactiveReason = inactiveReason?.toEnum(InactiveReason.UNKNOWN),
    hidden = hidden,
    lastUsedAt = lastUsedAt.toInstantOrNull(),
)

internal fun SendDto.toDomain() = Send(
    id = id,
    videoId = videoId,
    targetId = targetId,
    status = status.toEnum(SendStatus.SENDING),
    error = error?.toDomain(),
    createdAt = createdAt.toInstantOrNull(),
    updatedAt = updatedAt.toInstantOrNull(),
)

internal fun HistoryActionDto.toDomain() = HistoryAction(
    type = type.toEnum(HistoryActionType.UNKNOWN),
    targetTitle = targetTitle,
    sendId = sendId,
    at = at.toInstantOrNull(),
)

internal fun HistoryItemDto.toDomain(urls: ApiUrls) = HistoryItem(
    id = id,
    video = video.toDomain(urls),
    actions = actions.map { it.toDomain() },
)

internal fun HistoryPageDto.toDomain(urls: ApiUrls) = HistoryPage(items.map { it.toDomain(urls) }, nextCursor)

internal fun AppVersionDto.toDomain() =
    AppVersion(versionCode, versionName, apkUrl, sha256, minSupportedVersionCode, changelog)

internal fun WorkerDto.toDomain() = Worker(
    id = id,
    fingerprint = fingerprint,
    status = status.toEnum(WorkerStatus.REVOKED),
    connected = connected,
    version = version,
    ytdlpVersion = ytdlpVersion,
    lastSeenAt = lastSeenAt.toInstantOrNull(),
    isPoller = isPoller,
)

internal fun AdminStatusDto.toDomain() = AdminStatus(
    workers = workers.map { it.toDomain() },
    queued = queue.queued,
    running = queue.running,
    errors24h = PlatformErrors(
        tiktok = errors24h["tiktok"] ?: 0,
        instagram = errors24h["instagram"] ?: 0,
        telegram = errors24h["telegram"] ?: 0,
    ),
    disk = DiskUsage(disk.usedBytes, disk.totalBytes, disk.cacheBytes),
    coreVersion = versions["core"]?.takeIf { it.isNotEmpty() },
    appLatestVersion = versions["app_latest"]?.takeIf { it.isNotEmpty() },
)

internal fun InviteDto.toDomain() = Invite(
    id = id,
    role = role.toRole(),
    codePrefix = codePrefix,
    createdAt = createdAt.toInstantOrNull(),
    expiresAt = expiresAt.toInstantOrNull(),
    usedBy = usedBy,
    usedAt = usedAt.toInstantOrNull(),
)

internal fun CreatedInviteDto.toDomain() = CreatedInvite(code, expiresAt.toInstantOrNull())

internal fun AdminUserDto.toDomain() = AdminUser(
    id = id,
    role = role.toRole(),
    status = status.toEnum(UserStatus.ACTIVE),
    telegramUsername = telegramUsername,
    telegramFirstName = telegramFirstName,
    targetsCount = targetsCount,
    createdAt = createdAt.toInstantOrNull(),
    lastSeenAt = lastSeenAt.toInstantOrNull(),
)
