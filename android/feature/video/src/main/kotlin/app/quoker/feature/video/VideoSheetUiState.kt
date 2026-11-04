package app.quoker.feature.video

import app.quoker.core.common.QuokerError
import app.quoker.core.data.model.Platform
import app.quoker.core.data.model.Video
import app.quoker.core.download.DownloadState

/** What the "Видео" sheet opens: a shared/pasted text with a link, or a known video id (history). */
sealed interface VideoSource {
    data class Url(val url: String) : VideoSource

    data class Id(val videoId: String) : VideoSource

    /** Distinguishes ViewModels of different sources in one ViewModelStore. */
    val key: String
        get() = when (this) {
            is Url -> "video-sheet:url:$url"
            is Id -> "video-sheet:id:$videoId"
        }
}

sealed interface VideoContent {
    data object Loading : VideoContent

    data class Ready(val video: Video) : VideoContent

    /** [platform]/[url] are known from the link when it parsed. */
    data class Error(val error: QuokerError, val platform: Platform?, val url: String?) : VideoContent
}

sealed interface SendUi {
    data object Idle : SendUi

    /** Checking whether Telegram is linked before opening "Куда отправить". */
    data object Checking : SendUi

    data class Sending(val targetTitle: String) : SendUi

    data class Sent(val targetTitle: String) : SendUi

    data class Failed(val targetTitle: String, val error: QuokerError?) : SendUi
}

data class VideoSheetUiState(
    val content: VideoContent = VideoContent.Loading,
    val download: DownloadState = DownloadState.Idle,
    val send: SendUi = SendUi.Idle,
    /** Notifications are allowed (or not needed): the hint mentions the notification. */
    val notificationsShown: Boolean = true,
)

sealed interface VideoSheetEvent {
    data class OpenSend(val videoId: String) : VideoSheetEvent

    data object LinkTelegram : VideoSheetEvent

    data class RequestPermissions(val permissions: List<String>) : VideoSheetEvent
}
