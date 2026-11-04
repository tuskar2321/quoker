package app.quoker.core.designsystem.text

import android.content.res.Resources
import androidx.annotation.StringRes
import androidx.compose.runtime.Composable
import androidx.compose.runtime.ReadOnlyComposable
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import app.quoker.core.common.QuokerError
import app.quoker.core.common.format.ByteSize
import app.quoker.core.common.format.DayBucket
import app.quoker.core.common.format.SizeUnit
import app.quoker.core.designsystem.R
import java.time.format.DateTimeFormatter
import java.util.Locale

/** Codes of docs/03-public-api.md (plus the client-side `network`) mapped to `error_<code>`. */
private val errorStrings: Map<String, Int> = mapOf(
    "unauthorized" to R.string.error_unauthorized,
    "forbidden" to R.string.error_forbidden,
    "user_blocked" to R.string.error_user_blocked,
    "upgrade_required" to R.string.error_upgrade_required,
    "invalid_request" to R.string.error_invalid_request,
    "invalid_invite" to R.string.error_invalid_invite,
    "telegram_not_linked" to R.string.error_telegram_not_linked,
    "link_token_expired" to R.string.error_link_token_expired,
    "unsupported_url" to R.string.error_unsupported_url,
    "unsupported_content" to R.string.error_unsupported_content,
    "private_video" to R.string.error_private_video,
    "not_found" to R.string.error_not_found,
    "file_not_ready" to R.string.error_file_not_ready,
    "too_large" to R.string.error_too_large,
    "target_inactive" to R.string.error_target_inactive,
    "target_forbidden" to R.string.error_target_forbidden,
    "rate_limited" to R.string.error_rate_limited,
    "worker_unavailable" to R.string.error_worker_unavailable,
    "upstream_error" to R.string.error_upstream_error,
    "internal" to R.string.error_internal,
    "network" to R.string.error_network,
)

/** `R.string.error_<code>` for a known code, else null. */
@StringRes
fun errorStringRes(code: String): Int? = errorStrings[code]

/** UI text for the error: our `error_<code>`, else the server message, else a generic text. */
fun QuokerError.userMessage(resources: Resources): String {
    errorStringRes(code)?.let { return resources.getString(it) }
    return message.takeIf { it.isNotBlank() } ?: resources.getString(R.string.error_generic)
}

@Composable
@ReadOnlyComposable
fun QuokerError.userMessage(): String {
    LocalConfiguration.current // re-read on configuration change
    return userMessage(LocalContext.current.resources)
}

fun ByteSize.text(resources: Resources): String = resources.getString(
    when (unit) {
        SizeUnit.Bytes -> R.string.size_bytes
        SizeUnit.Kilobytes -> R.string.size_kilobytes
        SizeUnit.Megabytes -> R.string.size_megabytes
        SizeUnit.Gigabytes -> R.string.size_gigabytes
    },
    value,
)

/** «14 МБ». */
@Composable
@ReadOnlyComposable
fun formatFileSize(bytes: Long): String {
    LocalConfiguration.current
    return ByteSize.of(bytes).text(LocalContext.current.resources)
}

private val ru = Locale.forLanguageTag("ru")
private val dayMonth = DateTimeFormatter.ofPattern("d MMMM", ru)
private val dayMonthYear = DateTimeFormatter.ofPattern("d MMMM yyyy", ru)

fun DayBucket.label(resources: Resources): String = when (this) {
    DayBucket.Today -> resources.getString(R.string.day_today)
    DayBucket.Yesterday -> resources.getString(R.string.day_yesterday)
    is DayBucket.Date -> (if (sameYear) dayMonth else dayMonthYear).format(date)
}

/** «Сегодня», «Вчера», «9 ноября», «31 декабря 2025». */
@Composable
@ReadOnlyComposable
fun DayBucket.label(): String {
    LocalConfiguration.current
    return label(LocalContext.current.resources)
}
