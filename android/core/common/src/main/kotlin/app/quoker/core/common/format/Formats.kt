package app.quoker.core.common.format

import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.util.Locale
import kotlin.math.roundToLong

/** Units for [ByteSize]; the localized labels live in core:designsystem. */
enum class SizeUnit { Bytes, Kilobytes, Megabytes, Gigabytes }

/**
 * A size split into a display number and a unit (binary multiples). [value] uses a decimal comma
 * and has one fraction digit only below 10 (e.g. "1,2", "14", "512").
 */
data class ByteSize(val value: String, val unit: SizeUnit) {
    companion object {
        fun of(bytes: Long): ByteSize {
            require(bytes >= 0) { "negative size: $bytes" }
            var amount = bytes.toDouble()
            var unitIndex = 0
            val units = SizeUnit.entries
            while (amount >= 1024 && unitIndex < units.lastIndex) {
                amount /= 1024
                unitIndex++
            }
            val unit = units[unitIndex]
            val oneDecimal = unit != SizeUnit.Bytes && amount < 10
            val text = if (oneDecimal) {
                val tenths = (amount * 10).roundToLong()
                if (tenths % 10 == 0L) "${tenths / 10}" else "${tenths / 10},${tenths % 10}"
            } else {
                amount.roundToLong().toString()
            }
            return ByteSize(text, unit)
        }
    }
}

/** Formats a duration in seconds as "0:27", "12:05" or "1:02:03". */
fun formatDuration(totalSeconds: Long): String {
    val s = totalSeconds.coerceAtLeast(0)
    val hours = s / 3600
    val minutes = (s % 3600) / 60
    val seconds = s % 60
    return if (hours > 0) {
        String.format(Locale.ROOT, "%d:%02d:%02d", hours, minutes, seconds)
    } else {
        String.format(Locale.ROOT, "%d:%02d", minutes, seconds)
    }
}

/** Day group for history lists; labels («Сегодня», «Вчера», date) are in core:designsystem. */
sealed interface DayBucket {
    data object Today : DayBucket
    data object Yesterday : DayBucket
    data class Date(val date: LocalDate, val sameYear: Boolean) : DayBucket

    companion object {
        fun of(instant: Instant, now: Instant, zone: ZoneId = ZoneId.systemDefault()): DayBucket {
            val day = instant.atZone(zone).toLocalDate()
            val today = now.atZone(zone).toLocalDate()
            return when (day) {
                today -> Today
                today.minusDays(1) -> Yesterday
                else -> Date(day, day.year == today.year)
            }
        }
    }
}

/** Invite codes: `XXXX-XXXX`, upper-case letters and digits (docs/11-nfr-security.md). */
object InviteCode {
    const val LENGTH = 8
    private const val GROUP = 4

    /** Keeps letters/digits upper-cased (at most 8) and inserts the dash after the first group. */
    fun format(input: String): String {
        val raw = normalize(input)
        return if (raw.length > GROUP) raw.substring(0, GROUP) + "-" + raw.substring(GROUP) else raw
    }

    /** The bare characters, without the dash: what the user typed, cleaned up. */
    fun normalize(input: String): String = input.uppercase()
        .filter { it in 'A'..'Z' || it in '0'..'9' }
        .take(LENGTH)

    fun isComplete(input: String): Boolean = normalize(input).length == LENGTH
}
