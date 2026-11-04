package app.quoker.core.designsystem

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performScrollTo
import androidx.test.ext.junit.runners.AndroidJUnit4
import app.quoker.core.designsystem.component.AvatarTone
import app.quoker.core.designsystem.component.BadgeTone
import app.quoker.core.designsystem.component.Banner
import app.quoker.core.designsystem.component.BannerTone
import app.quoker.core.designsystem.component.ButtonSize
import app.quoker.core.designsystem.component.ClipboardChip
import app.quoker.core.designsystem.component.ConfirmDialog
import app.quoker.core.designsystem.component.CoverSize
import app.quoker.core.designsystem.component.DownloadProgressButton
import app.quoker.core.designsystem.component.EmptyState
import app.quoker.core.designsystem.component.FieldIconAction
import app.quoker.core.designsystem.component.IconContainer
import app.quoker.core.designsystem.component.InitialAvatar
import app.quoker.core.designsystem.component.ListGroup
import app.quoker.core.designsystem.component.ListRow
import app.quoker.core.designsystem.component.ListRowChevron
import app.quoker.core.designsystem.component.NoNetworkBanner
import app.quoker.core.designsystem.component.PlatformChip
import app.quoker.core.designsystem.component.PrimaryButton
import app.quoker.core.designsystem.component.PrimaryButtonVariant
import app.quoker.core.designsystem.component.QuokerBadge
import app.quoker.core.designsystem.component.QuokerCheckbox
import app.quoker.core.designsystem.component.QuokerIconButton
import app.quoker.core.designsystem.component.QuokerProgressBar
import app.quoker.core.designsystem.component.QuokerSwitch
import app.quoker.core.designsystem.component.QuokerTextArea
import app.quoker.core.designsystem.component.QuokerTextButton
import app.quoker.core.designsystem.component.QuokerTextField
import app.quoker.core.designsystem.component.ScreenHeader
import app.quoker.core.designsystem.component.SecondaryButton
import app.quoker.core.designsystem.component.SegmentedControl
import app.quoker.core.designsystem.component.SelectableRow
import app.quoker.core.designsystem.component.SheetBody
import app.quoker.core.designsystem.component.SkeletonBlock
import app.quoker.core.designsystem.component.SkeletonLine
import app.quoker.core.designsystem.component.StatusButton
import app.quoker.core.designsystem.component.StatusButtonAction
import app.quoker.core.designsystem.component.StatusTone
import app.quoker.core.designsystem.component.TopBar
import app.quoker.core.designsystem.component.VideoCover
import app.quoker.core.designsystem.icon.QuokerIcons
import app.quoker.core.designsystem.icon.QuokerLogo
import app.quoker.core.designsystem.theme.QuokerTheme
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class ComponentsSmokeTest {
    @get:Rule
    val compose = createComposeRule()

    private fun render(content: @Composable () -> Unit) {
        compose.setContent {
            QuokerTheme {
                Column(Modifier.verticalScroll(rememberScrollState())) { content() }
            }
        }
    }

    @Test
    fun buttonsRenderAndClick() {
        var clicks = 0
        render {
            PrimaryButton("Скачать", { clicks++ }, variant = PrimaryButtonVariant.Download, leadingIcon = QuokerIcons.Download)
            PrimaryButton("Отправить", {}, variant = PrimaryButtonVariant.Send)
            PrimaryButton("Ждём", { clicks++ }, loading = true)
            PrimaryButton("Нельзя", { clicks++ }, enabled = false)
            SecondaryButton("Закрыть", {})
            SecondaryButton("Скачать снова", {}, size = ButtonSize.Mini)
            QuokerTextButton("Вся история", {})
            QuokerIconButton(QuokerIcons.Settings, "Настройки", {})
        }
        compose.onNodeWithText("Скачать").performClick()
        compose.onNodeWithText("Нельзя").performClick()
        compose.onNodeWithText("Закрыть").assertIsDisplayed()
        compose.onNodeWithContentDescription("Настройки").assertIsDisplayed()
        assertEquals(1, clicks)
    }

    @Test
    fun fieldsAndChips() {
        render {
            QuokerTextField(
                "",
                {},
                label = "Ссылка",
                placeholder = "https://",
                trailing = { FieldIconAction(QuokerIcons.Paste, "Вставить", {}) },
            )
            QuokerTextField("ABCD", {}, isError = true, supportingText = "Код не подошёл")
            QuokerTextArea("подпись", {}, label = "Подпись")
            ClipboardChip("Вставить ссылку из буфера", "vm.tiktok.com/ZM…", {})
            PlatformChip("TikTok")
            QuokerBadge("Скачано", BadgeTone.Download)
            QuokerBadge("Ошибка отправки", BadgeTone.Error)
            VideoCover(null, CoverSize.Sheet, duration = "0:27", showPlay = true)
        }
        compose.onNodeWithText("https://").assertIsDisplayed()
        compose.onNodeWithText("Код не подошёл").assertIsDisplayed()
        compose.onNodeWithText("Вставить ссылку из буфера").assertIsDisplayed()
        compose.onNodeWithText("0:27").performScrollTo().assertIsDisplayed()
    }

    @Test
    fun listsAndHeaders() {
        render {
            TopBar { QuokerIconButton(QuokerIcons.Settings, "Настройки", {}) }
            QuokerLogo()
            ScreenHeader("История", onBack = {})
            ListGroup(header = "Telegram") {
                ListRow("Друзья", subtitle = "Группа", divider = true, trailing = { QuokerSwitch(true, {}) })
                ListRow("Админ", trailing = { ListRowChevron() }, onClick = {})
                ListRow("Чек", trailing = { QuokerCheckbox(false, {}) })
            }
            SelectableRow(true, {}, "Мой канал", subtitle = "Канал", leading = { InitialAvatar("М", tone = AvatarTone.SendSolid) })
            SegmentedControl(listOf("Пользователь", "Администратор"), 1, {})
            QuokerProgressBar(0.4f)
        }
        compose.onNodeWithText("quoker").assertIsDisplayed()
        compose.onNodeWithText("TELEGRAM").assertIsDisplayed()
        compose.onNodeWithContentDescription("Назад").assertIsDisplayed()
        compose.onNodeWithText("Администратор").performScrollTo().assertIsDisplayed()
    }

    @Test
    fun feedbackAndStatus() {
        var cancelled = false
        render {
            SheetBody {
                SkeletonBlock()
                SkeletonLine(widthFraction = 0.5f)
                DownloadProgressButton(62, { cancelled = true })
                StatusButton("Отправлено", StatusTone.Send, icon = QuokerIcons.Check, trailing = {
                    StatusButtonAction("Ещё", StatusTone.Send, {})
                })
                StatusButton("Отправляется…", StatusTone.Send, loading = true)
            }
            Banner("Ждём подтверждения", loading = true)
            Banner("Текст", tone = BannerTone.Error, title = "Заголовок")
            NoNetworkBanner(onRetry = {})
            IconContainer(QuokerIcons.Error)
            EmptyState("Пусто", "Ничего нет", actionText = "Обновить", onAction = {})
        }
        compose.onNodeWithText("Скачивается · 62%").assertIsDisplayed()
        compose.onNodeWithContentDescription("Отменить скачивание").performClick()
        compose.onNodeWithText("Нет сети. Проверьте подключение к интернету").performScrollTo().assertIsDisplayed()
        assertEquals(true, cancelled)
    }

    @Test
    fun confirmDialog() {
        var confirmed = false
        compose.setContent {
            QuokerTheme {
                ConfirmDialog("Выйти?", "Сессия завершится", "Выйти", { confirmed = true }, {}, destructive = true)
            }
        }
        compose.onNodeWithText("Отмена").assertIsDisplayed()
        compose.onNodeWithText("Выйти").performClick()
        assertEquals(true, confirmed)
    }
}
