package app.quoker.feature.settings.ui

import android.content.res.Resources
import androidx.compose.runtime.Composable
import androidx.compose.runtime.ReadOnlyComposable
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import app.quoker.core.common.format.DayBucket
import app.quoker.core.data.model.AdminUser
import app.quoker.feature.settings.R
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale

private val ru = Locale.forLanguageTag("ru")
private val dayMonth = DateTimeFormatter.ofPattern("d MMMM", ru)
private val dayMonthYear = DateTimeFormatter.ofPattern("d MMMM yyyy", ru)
private val hourMinute = DateTimeFormatter.ofPattern("HH:mm", ru)

/** «9 ноября» (with the year when it is not the current one). */
internal fun dateText(instant: Instant, now: Instant, zone: ZoneId = ZoneId.systemDefault()): String {
    val date = instant.atZone(zone).toLocalDate()
    val sameYear = date.year == now.atZone(zone).year
    return (if (sameYear) dayMonth else dayMonthYear).format(date)
}

/** «сегодня, 12:40», «вчера», «9 ноября» — lower case, for the middle of a line. */
internal fun lastSeenText(resources: Resources, instant: Instant, now: Instant, zone: ZoneId = ZoneId.systemDefault()): String =
    when (DayBucket.of(instant, now, zone)) {
        DayBucket.Today -> resources.getString(R.string.time_today_at, hourMinute.format(instant.atZone(zone)))
        DayBucket.Yesterday -> resources.getString(R.string.time_yesterday)
        is DayBucket.Date -> dateText(instant, now, zone)
    }

internal fun String.capitalizeFirst(): String = replaceFirstChar { it.titlecase(ru) }

@Composable
@ReadOnlyComposable
internal fun resources(): Resources {
    LocalConfiguration.current
    return LocalContext.current.resources
}

/** «@friend», else the Telegram first name, else «Пользователь #3». */
internal fun AdminUser.displayName(resources: Resources): String =
    telegramUsername?.takeIf { it.isNotBlank() }?.let { "@$it" }
        ?: telegramFirstName?.takeIf { it.isNotBlank() }
        ?: resources.getString(R.string.users_user_fallback, id)

internal val AdminUser.hasTelegram: Boolean
    get() = !telegramUsername.isNullOrBlank() || !telegramFirstName.isNullOrBlank()
