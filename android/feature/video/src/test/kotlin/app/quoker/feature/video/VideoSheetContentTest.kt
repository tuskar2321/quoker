package app.quoker.feature.video

import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.ext.junit.runners.AndroidJUnit4
import app.quoker.core.common.QuokerError
import app.quoker.core.data.model.Platform
import app.quoker.core.designsystem.component.SheetBody
import app.quoker.core.designsystem.theme.QuokerTheme
import app.quoker.core.download.DownloadState
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class VideoSheetContentTest {
    @get:Rule
    val compose = createComposeRule()

    private fun show(state: VideoSheetUiState, actions: VideoSheetActions = VideoSheetActions()) {
        compose.setContent { QuokerTheme { SheetBody { VideoSheetContent(state, actions) } } }
    }

    private val ready = VideoSheetUiState(content = VideoContent.Ready(video()))

    @Test
    fun readyShowsMockupTexts() {
        var downloads = 0
        var sends = 0
        show(ready, VideoSheetActions(onDownload = { downloads++ }, onSend = { sends++ }))
        compose.onNodeWithText("TikTok").assertExists()
        compose.onNodeWithText("@a").assertExists()
        compose.onNodeWithText("14 МБ · без водяного знака").assertExists()
        compose.onNodeWithText("vm.tiktok.com/ZM1").assertExists()
        compose.onNodeWithText("Открыть оригинал").assertExists()
        compose.onNodeWithText("Скачать").performClick()
        compose.onNodeWithText("Отправить в Telegram").performClick()
        assertEquals(1, downloads)
        assertEquals(1, sends)
    }

    @Test
    fun downloadingAndSentStates() {
        var again = 0
        show(ready.copy(download = DownloadState.Downloading(62), send = SendUi.Sent("Друзья")), VideoSheetActions(onSendAgain = { again++ }))
        compose.onNodeWithText("Скачивается · 62%").assertExists()
        compose.onNodeWithText("Можно закрыть — скачивание продолжится в фоне, прогресс будет в уведомлении.").assertExists()
        compose.onNodeWithText("Отправлено в «Друзья»").assertExists()
        compose.onNodeWithText("Ещё").performClick()
        assertEquals(1, again)
    }

    @Test
    fun savedAndSendingStates() {
        var opened: String? = null
        show(ready.copy(download = DownloadState.Saved("content://m/1"), send = SendUi.Sending("Друзья")), VideoSheetActions(onOpenSaved = { opened = it }))
        compose.onNodeWithText("Отправляется в «Друзья»…").assertExists()
        compose.onNodeWithText("Открыть").performClick()
        assertEquals("content://m/1", opened)
    }

    @Test
    fun sendFailedShowsRetry() {
        var retried: Long? = null
        show(ready.copy(send = SendUi.Failed("Друзья", null, sendId = 9)), VideoSheetActions(onRetrySend = { retried = it }))
        compose.onNodeWithText("Не отправлено").assertExists()
        compose.onNodeWithText("Повторить").performClick()
        assertEquals(9L, retried)
    }

    @Test
    fun resolveErrorShowsReasonAndActions() {
        var retries = 0
        show(
            VideoSheetUiState(content = VideoContent.Error(QuokerError("private_video", "", false), Platform.TIKTOK, "https://vm.tiktok.com/ZM1/")),
            VideoSheetActions(onRetry = { retries++ }),
        )
        compose.onNodeWithText("Не получилось открыть видео").assertExists()
        compose.onNodeWithText("Видео приватное или удалено. Проверьте, открывается ли оно в TikTok без входа в аккаунт.").assertExists()
        compose.onNodeWithText("Закрыть").assertExists()
        compose.onNodeWithText("Попробовать снова").performClick()
        assertEquals(1, retries)
    }
}
