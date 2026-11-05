package app.quoker.feature.history

import app.quoker.core.common.format.DayBucket
import app.quoker.core.data.model.HistoryActionType.DOWNLOADED
import app.quoker.core.data.model.HistoryActionType.RESOLVED
import app.quoker.core.data.model.HistoryActionType.SENT
import app.quoker.core.data.model.HistoryActionType.SEND_FAILED
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import org.junit.Assert.assertEquals
import org.junit.Test

class HistoryCardsTest {
    private val now = Instant.parse("2026-11-04T15:00:00Z")
    private val zone = ZoneId.of("Europe/Moscow")
    private val t = now.minusSeconds(3600)

    @Test
    fun `card badges and buttons`() {
        data class Case(
            val name: String,
            val item: app.quoker.core.data.model.HistoryItem,
            val downloaded: Boolean,
            val sentTo: List<String>,
            val failed: Boolean,
            val retrySendId: Long? = null,
        )
        val cases = listOf(
            Case("resolved only", item(1, action(RESOLVED, t)), false, emptyList(), false),
            Case(
                "downloaded and sent",
                item(2, action(SENT, t, "Друзья"), action(DOWNLOADED, t)),
                true, listOf("Друзья"), false,
            ),
            Case("failed", item(3, action(SEND_FAILED, t, "Друзья", sendId = 30)), false, emptyList(), true, 30),
            Case(
                "failed then sent to the same target",
                item(4, action(SENT, t, "Друзья", sendId = 41), action(SEND_FAILED, t, "Друзья", sendId = 40)),
                false, listOf("Друзья"), false,
            ),
            Case(
                "sent to one, failed to another",
                item(5, action(SEND_FAILED, t, "Канал", sendId = 52), action(SENT, t, "Друзья"), action(SENT, t, "Друзья")),
                false, listOf("Друзья"), true, 52,
            ),
            Case(
                "sent to one after it failed, older failure to another is retried",
                item(
                    6,
                    action(SENT, t, "Друзья", sendId = 63),
                    action(SEND_FAILED, t, "Друзья", sendId = 62),
                    action(SEND_FAILED, t, "Канал", sendId = 61),
                ),
                false, listOf("Друзья"), true, 61,
            ),
        )
        for (c in cases) {
            val card = c.item.toCard()
            assertEquals(c.name, c.downloaded, card.downloaded)
            assertEquals(c.name, c.sentTo, card.sentTo)
            assertEquals(c.name, c.failed, card.sendFailed)
            assertEquals(c.name, c.retrySendId, card.retrySendId)
        }
    }

    @Test
    fun `size falls back to the file size`() {
        assertEquals(100L, item(1, action(RESOLVED, t)).toCard().sizeBytes)
    }

    @Test
    fun `sections group consecutive days, items without time join the previous one`() {
        val today = Instant.parse("2026-11-04T09:00:00Z")
        val yesterday = Instant.parse("2026-11-03T20:00:00Z")
        val older = Instant.parse("2026-10-30T10:00:00Z")
        val items = listOf(
            item(1, action(DOWNLOADED, today)),
            item(2, action(RESOLVED, null)),
            item(3, action(SENT, yesterday, "A")),
            item(4, action(SENT, older, "A")),
            item(4, action(SENT, older, "A")), // duplicate across pages
        )
        val sections = buildSections(items, now, zone)
        assertEquals(
            listOf(DayBucket.Today, DayBucket.Yesterday, DayBucket.Date(LocalDate.of(2026, 10, 30), sameYear = true)),
            sections.map { it.bucket },
        )
        assertEquals(listOf(listOf(1L, 2L), listOf(3L), listOf(4L)), sections.map { s -> s.cards.map { it.id } })
    }

    @Test
    fun `first item without time goes to today`() {
        assertEquals(DayBucket.Today, buildSections(listOf(item(1)), now, zone).single().bucket)
    }
}
