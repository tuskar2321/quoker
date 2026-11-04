package app.quoker.feature.home

import app.quoker.core.data.model.HistoryActionType.DOWNLOADED
import app.quoker.core.data.model.HistoryActionType.RESOLVED
import app.quoker.core.data.model.HistoryActionType.SENT
import app.quoker.core.data.model.HistoryActionType.SEND_FAILED
import org.junit.Assert.assertEquals
import org.junit.Test

class RecentMappingTest {
    @Test
    fun badgesFromActions() {
        data class Case(val name: String, val actions: List<app.quoker.core.data.model.HistoryAction>, val downloaded: Boolean, val sent: List<String>, val failed: Boolean)
        val cases = listOf(
            Case("resolved only", listOf(action(RESOLVED)), false, emptyList(), false),
            Case("downloaded and sent", listOf(action(SENT, "Друзья"), action(DOWNLOADED), action(SENT, "Друзья")), true, listOf("Друзья"), false),
            Case("failed latest", listOf(action(SEND_FAILED, "Канал"), action(SENT, "Друзья")), false, listOf("Друзья"), true),
            Case("failed then sent", listOf(action(SENT, "Канал"), action(SEND_FAILED, "Канал")), false, listOf("Канал"), false),
        )
        for (case in cases) {
            val item = historyItem(1, *case.actions.toTypedArray()).toRecent()
            assertEquals(case.name, case.downloaded, item.downloaded)
            assertEquals(case.name, case.sent, item.sentTo)
            assertEquals(case.name, case.failed, item.sendFailed)
        }
    }

    @Test
    fun authorWithoutAt() {
        val item = historyItem(1, action(RESOLVED)).copy(video = video("1", author = "@max")).toRecent()
        assertEquals("max", item.author)
        assertEquals(null, historyItem(1).copy(video = video("1", author = " ")).toRecent().author)
    }
}
