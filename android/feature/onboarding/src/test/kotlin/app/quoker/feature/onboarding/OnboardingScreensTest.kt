package app.quoker.feature.onboarding

import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.ext.junit.runners.AndroidJUnit4
import app.quoker.core.common.QuokerError
import app.quoker.core.designsystem.theme.QuokerTheme
import app.quoker.feature.onboarding.invite.InviteContent
import app.quoker.feature.onboarding.invite.InviteUiState
import app.quoker.feature.onboarding.telegram.TelegramLinkContent
import app.quoker.feature.onboarding.telegram.TelegramLinkPhase
import app.quoker.feature.onboarding.telegram.TelegramLinkUiState
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class OnboardingScreensTest {
    @get:Rule
    val compose = createComposeRule()

    @Test
    fun inviteShowsErrorAndDisablesIncompleteCode() {
        compose.setContent {
            QuokerTheme {
                InviteContent(InviteUiState(code = "K7Q2", error = QuokerError("invalid_invite", "", false)), {}, {})
            }
        }
        compose.onNodeWithText("Код не подошёл").assertExists()
        compose.onNodeWithText("Продолжить").assertIsNotEnabled()
    }

    @Test
    fun telegramExpiredRenewsAndSkipShown() {
        var renews = 0
        var skips = 0
        compose.setContent {
            QuokerTheme {
                TelegramLinkContent(
                    TelegramLinkUiState(phase = TelegramLinkPhase.Expired),
                    showSkip = true,
                    onOpenBot = {},
                    onRenew = { renews++ },
                    onRetry = {},
                    onSkip = { skips++ },
                    onBack = {},
                )
            }
        }
        compose.onNodeWithText("Ссылка истекла — получить новую").performClick()
        compose.onNodeWithText("Пропустить").performClick()
        assertEquals(1, renews)
        assertEquals(1, skips)
    }

    @Test
    fun telegramWaitingStatus() {
        compose.setContent {
            QuokerTheme {
                TelegramLinkContent(TelegramLinkUiState(phase = TelegramLinkPhase.Waiting), false, {}, {}, {}, {}, {})
            }
        }
        compose.onNodeWithText("Ждём подтверждения в Telegram…").assertExists()
        compose.onNodeWithText("Пропустить").assertDoesNotExist()
    }
}
