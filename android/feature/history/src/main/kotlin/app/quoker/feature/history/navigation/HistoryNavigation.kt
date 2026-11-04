package app.quoker.feature.history.navigation

import androidx.navigation.NavController
import androidx.navigation.NavGraphBuilder
import androidx.navigation.NavOptions
import androidx.navigation.compose.composable
import app.quoker.feature.history.HistoryScreen
import kotlinx.serialization.Serializable

@Serializable
data object HistoryRoute

fun NavController.navigateToHistory(navOptions: NavOptions? = null) = navigate(HistoryRoute, navOptions)

/**
 * The «История» screen. [onDownload] and [onSend] get the video id («Скачать снова»,
 * «Отправить», and «Повторить» for a failed send — the user picks the target again);
 * [onOpenVideo] opens the video sheet.
 */
fun NavGraphBuilder.historyScreen(
    onBack: () -> Unit,
    onOpenVideo: (videoId: String) -> Unit,
    onDownload: (videoId: String) -> Unit,
    onSend: (videoId: String) -> Unit,
) {
    composable<HistoryRoute> {
        HistoryScreen(onBack = onBack, onOpenVideo = onOpenVideo, onDownload = onDownload, onSend = onSend)
    }
}
