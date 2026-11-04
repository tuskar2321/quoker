package app.quoker.feature.send

/** Telegram's caption limit, counted in UTF-16 code units (an emoji counts as two). */
const val CAPTION_MAX_LENGTH = 1024

/** Remaining-characters hint appears when fewer than this many units are left. */
internal const val CAPTION_HINT_THRESHOLD = 100

/**
 * Cuts [text] to [CAPTION_MAX_LENGTH] UTF-16 units without splitting a surrogate pair.
 * Kotlin's String.length already counts UTF-16 units, exactly like Telegram.
 */
fun limitCaption(text: String, max: Int = CAPTION_MAX_LENGTH): String {
    if (text.length <= max) return text
    val end = if (Character.isHighSurrogate(text[max - 1])) max - 1 else max
    return text.substring(0, end)
}
