package app.quoker.feature.history

import app.quoker.core.common.format.DayBucket
import app.quoker.core.data.model.HistoryActionType
import app.quoker.core.data.model.HistoryItem
import app.quoker.core.data.model.Platform
import java.time.Instant
import java.time.ZoneId

/** One card of the history list, everything the UI shows already derived. */
data class HistoryCard(
    val id: Long,
    val videoId: String,
    val author: String?,
    val platform: Platform,
    val durationSeconds: Long?,
    val sizeBytes: Long?,
    val thumbnailUrl: String?,
    /** Time of the newest action; null when the core did not send one. */
    val lastActionAt: Instant?,
    val downloaded: Boolean,
    /** Titles of targets the video was sent to, newest first, without repeats. */
    val sentTo: List<String>,
    /** The latest send to some target failed: badge «Ошибка отправки», button «Повторить». */
    val sendFailed: Boolean,
)

data class HistorySection(val bucket: DayBucket, val cards: List<HistoryCard>)

fun HistoryItem.toCard(): HistoryCard {
    val sendActions = actions.filter { it.type == HistoryActionType.SENT || it.type == HistoryActionType.SEND_FAILED }
    // Actions are newest first: the first one per target is its current outcome.
    val latestPerTarget = sendActions.distinctBy { it.targetTitle }
    return HistoryCard(
        id = id,
        videoId = video.id,
        author = video.author?.takeIf { it.isNotBlank() },
        platform = video.platform,
        durationSeconds = video.durationSeconds,
        sizeBytes = video.sizeBytes ?: video.file.sizeBytes,
        thumbnailUrl = video.thumbnailUrl,
        lastActionAt = actions.firstNotNullOfOrNull { it.at },
        downloaded = actions.any { it.type == HistoryActionType.DOWNLOADED },
        sentTo = sendActions.filter { it.type == HistoryActionType.SENT }.mapNotNull { it.targetTitle }.distinct(),
        sendFailed = latestPerTarget.any { it.type == HistoryActionType.SEND_FAILED },
    )
}

/**
 * Groups items (ordered by their latest action) into «Сегодня / Вчера / дата» sections. An item
 * without a time joins the section before it.
 */
fun buildSections(items: List<HistoryItem>, now: Instant, zone: ZoneId): List<HistorySection> {
    val sections = mutableListOf<HistorySection>()
    for (card in items.distinctBy { it.id }.map { it.toCard() }) {
        val bucket = card.lastActionAt?.let { DayBucket.of(it, now, zone) }
            ?: sections.lastOrNull()?.bucket
            ?: DayBucket.Today
        val last = sections.lastOrNull()
        if (last != null && last.bucket == bucket) {
            sections[sections.lastIndex] = last.copy(cards = last.cards + card)
        } else {
            sections += HistorySection(bucket, listOf(card))
        }
    }
    return sections
}
