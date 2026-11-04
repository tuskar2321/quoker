package app.quoker.feature.settings

import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.test.ext.junit.runners.AndroidJUnit4
import app.quoker.core.data.model.InactiveReason
import app.quoker.core.data.model.Role
import app.quoker.core.data.model.TargetType
import app.quoker.core.data.model.UserStatus
import app.quoker.core.designsystem.theme.QuokerTheme
import app.quoker.feature.settings.admin.AdminStatusActions
import app.quoker.feature.settings.admin.AdminStatusContent
import app.quoker.feature.settings.admin.AdminStatusUiState
import app.quoker.feature.settings.admin.InvitesActions
import app.quoker.feature.settings.admin.InvitesContent
import app.quoker.feature.settings.admin.InvitesUiState
import app.quoker.feature.settings.admin.NewInvite
import app.quoker.feature.settings.admin.UsersContent
import app.quoker.feature.settings.admin.UsersUiState
import java.time.Instant
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config

@RunWith(AndroidJUnit4::class)
@Config(qualifiers = "ru")
class SettingsScreensTest {
    @get:Rule
    val compose = createComposeRule()

    private val now = Instant.parse("2026-11-05T12:00:00Z")

    @Test
    fun settingsShowsAdminRowAndInactiveReason() {
        var admin = 0
        compose.setContent {
            QuokerTheme {
                SettingsContent(
                    SettingsUiState(
                        me = me(admin = true),
                        targets = listOf(TargetRow(target(4, reason = InactiveReason.BOT_REMOVED, type = TargetType.CHANNEL), true, false)),
                        targetsLoaded = true,
                        versionName = "1.2.0",
                    ),
                    SettingsActions(onOpenAdmin = { admin++ }),
                )
            }
        }
        compose.onNodeWithText("Бот удалён из канала").assertExists()
        compose.onNodeWithText("@max").assertExists()
        compose.onNodeWithText("1.2.0").assertExists()
        compose.onNodeWithText("Администрирование").performScrollTo().performClick()
        assertEquals(1, admin)
    }

    @Test
    fun settingsHidesAdminRowAndOffersLinkForUnlinkedUser() {
        var link = 0
        compose.setContent {
            QuokerTheme {
                SettingsContent(
                    SettingsUiState(me = me(linked = false), targetsLoaded = true),
                    SettingsActions(onLinkTelegram = { link++ }),
                )
            }
        }
        compose.onNodeWithText("Администрирование").assertDoesNotExist()
        compose.onNodeWithText("Пока некуда отправлять").assertExists()
        compose.onNodeWithText("Привязать Telegram").performScrollTo().performClick()
        assertEquals(1, link)
    }

    @Test
    fun adminStatusShowsAlertAndStaleCookies() {
        compose.setContent {
            QuokerTheme {
                AdminStatusContent(
                    AdminStatusUiState(
                        status = adminStatus(instagramErrors = 3),
                        cookiesUploadedAt = now.minusSeconds(21L * 24 * 3600),
                        now = now,
                    ),
                    AdminStatusActions(),
                )
            }
        }
        compose.onNodeWithText("Instagram требует вход").assertExists()
        compose.onNodeWithText("TikTok 0 · Reels 3 · TG 0").assertExists()
        compose.onNodeWithText("Обновлены 21 день назад").assertExists()
        compose.onNodeWithText("41%").assertExists()
    }

    @Test
    fun newInviteCodeCopies() {
        var copied: String? = null
        compose.setContent {
            QuokerTheme {
                InvitesContent(
                    InvitesUiState(
                        created = NewInvite("K7Q2-M4XP", Role.USER, Instant.parse("2026-11-09T12:00:00Z")),
                        invites = emptyList(),
                        now = now,
                    ),
                    InvitesActions(onCopy = { copied = it }),
                )
            }
        }
        compose.onNodeWithText("Новый код · пользователь · до 9 ноября").assertExists()
        compose.onNodeWithText("Скопировать").performScrollTo().performClick()
        assertEquals("K7Q2-M4XP", copied)
        compose.onNodeWithText("Скопировано").assertExists()
    }

    @Test
    fun usersOwnAccountHasNoActions() {
        compose.setContent {
            QuokerTheme {
                UsersContent(
                    UsersUiState(
                        users = listOf(user(1, role = Role.ADMIN), user(2), user(3, UserStatus.BLOCKED)),
                        myId = 1,
                        now = now,
                    ),
                )
            }
        }
        compose.onNodeWithText("Админ · это вы").assertExists()
        compose.onNodeWithText("3 человека · 2 активны · 1 заблокирован").assertExists()
        assertEquals(1, compose.onAllNodesWithText("Заблокировать").fetchSemanticsNodes().size)
        compose.onNodeWithText("Разблокировать").assertExists()
    }
}
