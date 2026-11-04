package app.quoker.feature.home

import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.test.ext.junit.runners.AndroidJUnit4
import app.quoker.core.common.link.LinkExtractor
import app.quoker.core.data.model.Platform
import app.quoker.core.designsystem.theme.QuokerTheme
import java.time.Instant
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class HomeScreenTest {
    @get:Rule
    val compose = createComposeRule()

    private val now = Instant.parse("2026-11-04T12:00:00Z")

    private fun render(state: HomeUiState, onHistory: () -> Unit = {}, onRecent: (RecentItem) -> Unit = {}) {
        compose.setContent {
            QuokerTheme { HomeContent(state, {}, {}, {}, {}, onRecent, onHistory, {}, now = now) }
        }
    }

    @Test
    fun emptyStateHidesRecentAndChip() {
        render(HomeUiState())
        compose.onNodeWithText("Недавние").assertDoesNotExist()
        compose.onNodeWithText("Вставить ссылку из буфера").assertDoesNotExist()
        compose.onNodeWithText("Нет сети. Проверьте подключение к интернету").assertDoesNotExist()
    }

    @Test
    fun recentChipBannersAndError() {
        var history = 0
        var opened: RecentItem? = null
        val item = RecentItem(1, "https://x", null, "author", Platform.INSTAGRAM, true, listOf("Друзья"), false, now)
        render(
            HomeUiState(
                link = "https://example.com",
                linkError = "unsupported_url",
                clipboardLink = LinkExtractor.firstSupported("https://vm.tiktok.com/ZMabc123/"),
                recent = listOf(item),
                offline = true,
                noWorkers = true,
            ),
            onHistory = { history++ },
            onRecent = { opened = it },
        )
        compose.onNodeWithText("Вставить ссылку из буфера").assertExists()
        compose.onNodeWithText("Это не видео TikTok или Reels").assertExists()
        compose.onNodeWithText("Нет сети. Проверьте подключение к интернету").assertExists()
        compose.onNodeWithText("Нет подключённых воркеров").assertExists()
        compose.onNodeWithText("Скачано").assertExists()
        compose.onNodeWithText("→ Друзья").assertExists()
        compose.onNodeWithText("@author · Reels").performScrollTo().performClick()
        compose.onNodeWithText("Вся история").performScrollTo().performClick()
        assertEquals(item, opened)
        assertEquals(1, history)
    }
}
