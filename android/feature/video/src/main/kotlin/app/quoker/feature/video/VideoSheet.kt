package app.quoker.feature.video

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.widget.Toast
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import app.quoker.core.common.QuokerError
import app.quoker.core.common.format.formatDuration
import app.quoker.core.data.model.FileState
import app.quoker.core.data.model.FileStatus
import app.quoker.core.data.model.Platform
import app.quoker.core.data.model.Video
import app.quoker.core.designsystem.R as DesignR
import app.quoker.core.designsystem.component.ButtonTone
import app.quoker.core.designsystem.component.ConfirmDialog
import app.quoker.core.designsystem.component.CoverSize
import app.quoker.core.designsystem.component.DownloadProgressButton
import app.quoker.core.designsystem.component.IconContainer
import app.quoker.core.designsystem.component.IconContainerTone
import app.quoker.core.designsystem.component.PlatformChip
import app.quoker.core.designsystem.component.PrimaryButton
import app.quoker.core.designsystem.component.PrimaryButtonVariant
import app.quoker.core.designsystem.component.QuokerBottomSheet
import app.quoker.core.designsystem.component.QuokerTextButton
import app.quoker.core.designsystem.component.SecondaryButton
import app.quoker.core.designsystem.component.SheetBody
import app.quoker.core.designsystem.component.SkeletonBlock
import app.quoker.core.designsystem.component.SkeletonLine
import app.quoker.core.designsystem.component.StatusButton
import app.quoker.core.designsystem.component.StatusButtonAction
import app.quoker.core.designsystem.component.StatusButtonIconAction
import app.quoker.core.designsystem.component.StatusTone
import app.quoker.core.designsystem.component.VideoCover
import app.quoker.core.designsystem.icon.QuokerIcons
import app.quoker.core.designsystem.text.formatFileSize
import app.quoker.core.designsystem.text.userMessage
import app.quoker.core.designsystem.theme.QuokerTheme
import app.quoker.core.download.DownloadErrors
import app.quoker.core.download.DownloadState
import app.quoker.core.download.VideoDownloads
import app.quoker.core.download.downloadMessage
import app.quoker.core.network.ErrorCodes

/**
 * The "Видео" sheet (Video / Downloading / Error mockups), shared by MainActivity and ShareActivity.
 * [onSend] opens "Куда отправить" for the video (with
 * `retrySendId` for «Повторить» of a failed send: its target is preselected); [onLinkTelegram] opens the Telegram binding when
 * the account has none.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun VideoSheet(
    source: VideoSource,
    onSend: (videoId: String, retrySendId: Long?) -> Unit,
    onLinkTelegram: () -> Unit,
    onDismiss: () -> Unit,
    modifier: Modifier = Modifier,
    downloadOnOpen: Boolean = false,
    onDownloadOnOpenConsumed: () -> Unit = {},
) {
    val viewModel = hiltViewModel<VideoSheetViewModel, VideoSheetViewModel.Factory>(
        key = source.key,
        creationCallback = { factory -> factory.create(source) },
    )
    val state by viewModel.state.collectAsStateWithLifecycle()
    val context = LocalContext.current
    val currentOnSend by rememberUpdatedState(onSend)
    val currentOnLinkTelegram by rememberUpdatedState(onLinkTelegram)
    val permissions = rememberLauncherForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) {
        viewModel.onPermissionsResult(it)
    }

    LaunchedEffect(viewModel) {
        viewModel.eventFlow.collect { event ->
            when (event) {
                is VideoSheetEvent.OpenSend -> currentOnSend(event.videoId, event.retrySendId)
                VideoSheetEvent.LinkTelegram -> currentOnLinkTelegram()
                is VideoSheetEvent.RequestPermissions -> permissions.launch(event.permissions.toTypedArray())
            }
        }
    }

    // Before the download request below: it resets what the previous showing left behind.
    DisposableEffect(viewModel) {
        viewModel.onShown()
        onDispose { }
    }

    LaunchedEffect(viewModel, downloadOnOpen) {
        if (downloadOnOpen) {
            viewModel.onDownloadRequested()
            onDownloadOnOpenConsumed()
        }
    }

    if (state.confirmRedownload) {
        ConfirmDialog(
            title = stringResource(R.string.video_redownload_title),
            text = null,
            confirmText = stringResource(R.string.video_redownload_confirm),
            onConfirm = viewModel::onConfirmRedownload,
            onDismiss = viewModel::onDismissRedownload,
        )
    }

    QuokerBottomSheet(
        onDismissRequest = onDismiss,
        modifier = modifier,
        spacing = if (state.content is VideoContent.Error) 16.dp else QuokerTheme.dimens.sheetGap,
    ) {
        VideoSheetContent(
            state = state,
            actions = VideoSheetActions(
                onDownload = viewModel::onDownloadClick,
                onCancelDownload = viewModel::onCancelDownload,
                onOpenSaved = { uri -> context.openOrToast(VideoDownloads.openSavedVideoIntent(uri)) },
                onSend = viewModel::onSendClick,
                onSendAgain = viewModel::onSendAgain,
                onRetrySend = viewModel::onRetrySend,
                onOpenOriginal = { url ->
                    context.openOrToast(Intent(Intent.ACTION_VIEW, Uri.parse(url)).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
                },
                onRetry = viewModel::onRetry,
                onClose = onDismiss,
            ),
        )
    }
}

internal data class VideoSheetActions(
    val onDownload: () -> Unit = {},
    val onCancelDownload: () -> Unit = {},
    val onOpenSaved: (uri: String) -> Unit = {},
    val onSend: () -> Unit = {},
    val onSendAgain: () -> Unit = {},
    val onRetrySend: (sendId: Long) -> Unit = {},
    val onOpenOriginal: (url: String) -> Unit = {},
    val onRetry: () -> Unit = {},
    val onClose: () -> Unit = {},
)

/** Sheet content without the modal host (also used by previews). */
@Composable
internal fun ColumnScope.VideoSheetContent(state: VideoSheetUiState, actions: VideoSheetActions) {
    when (val content = state.content) {
        VideoContent.Loading -> LoadingContent()
        is VideoContent.Ready -> ReadyContent(content.video, state, actions)
        is VideoContent.Error -> ErrorContent(content, actions)
    }
}

@Composable
private fun ColumnScope.LoadingContent() {
    val d = QuokerTheme.dimens
    Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) {
        SkeletonBlock(Modifier.size(d.coverSheet), RoundedCornerShape(d.coverSheetRadius))
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            SkeletonBlock(Modifier.size(width = 64.dp, height = 24.dp), RoundedCornerShape(d.badgeRadius))
            SkeletonLine(widthFraction = 0.6f, height = 18.dp)
            SkeletonLine()
            SkeletonLine()
            SkeletonLine(widthFraction = 0.7f)
        }
    }
    Column(verticalArrangement = Arrangement.spacedBy(d.buttonGap)) {
        PrimaryButton(
            stringResource(R.string.video_action_download), {},
            variant = PrimaryButtonVariant.Download, leadingIcon = QuokerIcons.Download, enabled = false,
        )
        PrimaryButton(
            stringResource(R.string.video_action_send), {},
            variant = PrimaryButtonVariant.Send, leadingIcon = QuokerIcons.Send, enabled = false,
        )
    }
    SkeletonLine(Modifier.padding(vertical = 12.dp), widthFraction = 0.4f, height = 13.dp)
}

@Composable
private fun ColumnScope.ReadyContent(video: Video, state: VideoSheetUiState, actions: VideoSheetActions) {
    val d = QuokerTheme.dimens
    val c = QuokerTheme.colors
    val t = QuokerTheme.typography
    Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) {
        VideoCover(
            model = video.thumbnailUrl,
            size = CoverSize.Sheet,
            duration = video.durationSeconds?.let(::formatDuration),
            showPlay = true,
            contentDescription = stringResource(R.string.video_cover_description),
        )
        Column(
            modifier = Modifier.weight(1f).height(d.coverSheet.height),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            platformName(video.platform)?.let { PlatformChip(it) }
            video.author?.takeIf { it.isNotBlank() }?.let {
                Text(
                    stringResource(R.string.video_author, it.removePrefix("@")),
                    style = t.titleSmall.copy(fontWeight = FontWeight.W800),
                    color = c.onSurface,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            video.caption?.takeIf { it.isNotBlank() }?.let {
                Text(it, style = t.bodyMedium, color = c.onSurfaceStrong, maxLines = 3, overflow = TextOverflow.Ellipsis)
            }
            Spacer(Modifier.weight(1f))
            metaLine(video)?.let { Text(it, style = t.bodySmall, color = c.onSurfaceVariant) }
        }
    }

    Column(verticalArrangement = Arrangement.spacedBy(d.buttonGap)) {
        DownloadControl(state.download, actions)
        SendControl(state.send, actions)
    }

    when (val download = state.download) {
        DownloadState.Preparing, is DownloadState.Downloading -> Text(
            stringResource(if (state.notificationsShown) R.string.video_download_hint else R.string.video_download_hint_no_notifications),
            style = t.bodySmall,
            color = c.onSurfaceVariant,
        )
        is DownloadState.Failed -> {
            Text(downloadErrorText(download.error), style = t.bodySmall, color = c.error)
            UrlRow(video, actions)
        }
        else -> UrlRow(video, actions)
    }
}

@Composable
private fun UrlRow(video: Video, actions: VideoSheetActions) {
    val c = QuokerTheme.colors
    Row(
        modifier = Modifier.fillMaxWidth(),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(
            displayUrl(video.sourceUrl.ifBlank { video.canonicalUrl }),
            style = QuokerTheme.typography.bodySmall,
            color = c.onSurfaceVariant,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.weight(1f),
        )
        QuokerTextButton(
            stringResource(R.string.video_action_open_original),
            onClick = { actions.onOpenOriginal(video.canonicalUrl.ifBlank { video.sourceUrl }) },
        )
    }
}

@Composable
private fun DownloadControl(download: DownloadState, actions: VideoSheetActions) {
    when (download) {
        DownloadState.Idle -> PrimaryButton(
            stringResource(R.string.video_action_download),
            actions.onDownload,
            variant = PrimaryButtonVariant.Download,
            leadingIcon = QuokerIcons.Download,
        )
        DownloadState.Preparing -> StatusButton(
            text = stringResource(R.string.video_download_preparing),
            tone = StatusTone.Download,
            loading = true,
            trailing = {
                StatusButtonIconAction(
                    QuokerIcons.Close, stringResource(DesignR.string.download_cancel), StatusTone.Download, actions.onCancelDownload,
                )
            },
        )
        is DownloadState.Downloading -> DownloadProgressButton(download.percent, actions.onCancelDownload)
        is DownloadState.Saved -> StatusButton(
            text = stringResource(R.string.video_download_saved),
            tone = StatusTone.Download,
            icon = QuokerIcons.Check,
            onClick = { actions.onOpenSaved(download.uri) },
            trailing = {
                StatusButtonAction(stringResource(DesignR.string.action_open), StatusTone.Download, { actions.onOpenSaved(download.uri) })
            },
        )
        is DownloadState.Failed -> StatusButton(
            text = stringResource(R.string.video_download_failed),
            tone = StatusTone.Error,
            icon = QuokerIcons.Error,
            trailing = { StatusButtonAction(stringResource(DesignR.string.action_retry), StatusTone.Error, actions.onDownload) },
        )
    }
}

@Composable
private fun SendControl(send: SendUi, actions: VideoSheetActions) {
    when (send) {
        SendUi.Idle, SendUi.Checking -> PrimaryButton(
            stringResource(R.string.video_action_send),
            actions.onSend,
            variant = PrimaryButtonVariant.Send,
            leadingIcon = QuokerIcons.Send,
            loading = send == SendUi.Checking,
        )
        is SendUi.Sending -> StatusButton(
            text = stringResource(R.string.video_send_sending, send.targetTitle),
            tone = StatusTone.Send,
            loading = true,
        )
        is SendUi.Sent -> StatusButton(
            text = stringResource(R.string.video_send_sent, send.targetTitle),
            tone = StatusTone.Send,
            icon = QuokerIcons.Check,
            trailing = { StatusButtonAction(stringResource(DesignR.string.action_more), StatusTone.Send, actions.onSendAgain) },
        )
        is SendUi.Failed -> StatusButton(
            text = stringResource(R.string.video_send_failed),
            tone = StatusTone.Error,
            icon = QuokerIcons.Error,
            trailing = { StatusButtonAction(stringResource(DesignR.string.action_retry), StatusTone.Error, onClick = { actions.onRetrySend(send.sendId) }) },
        )
    }
}

@Composable
private fun ColumnScope.ErrorContent(content: VideoContent.Error, actions: VideoSheetActions) {
    val c = QuokerTheme.colors
    val t = QuokerTheme.typography
    IconContainer(QuokerIcons.Error, Modifier.padding(top = 8.dp), tone = IconContainerTone.Error)
    Text(stringResource(R.string.video_error_title), style = t.titleLarge, color = c.onSurface)
    Text(resolveErrorText(content.error, content.platform), style = t.bodyParagraph, color = c.onSurfaceStrong)
    content.url?.let {
        Text(displayUrl(it), style = t.bodySmall, color = c.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
    Column(Modifier.padding(top = 8.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        PrimaryButton(stringResource(DesignR.string.action_try_again), actions.onRetry, variant = PrimaryButtonVariant.Neutral)
        SecondaryButton(stringResource(DesignR.string.action_close), actions.onClose, tone = ButtonTone.Default)
    }
}

@Composable
private fun resolveErrorText(error: QuokerError, platform: Platform?): String = when {
    error.code == ErrorCodes.PRIVATE_VIDEO && platform == Platform.TIKTOK -> stringResource(R.string.video_error_private_tiktok)
    error.code == ErrorCodes.PRIVATE_VIDEO && platform == Platform.INSTAGRAM -> stringResource(R.string.video_error_private_instagram)
    error.code == ErrorCodes.RATE_LIMITED -> stringResource(R.string.video_error_rate_limited)
    else -> error.userMessage()
}

@Composable
private fun downloadErrorText(error: QuokerError): String = when (error.code) {
    DownloadErrors.STORAGE, DownloadErrors.STORAGE_PERMISSION -> error.downloadMessage(LocalContext.current.resources)
    else -> error.userMessage()
}

@Composable
private fun platformName(platform: Platform): String? = when (platform) {
    Platform.TIKTOK -> stringResource(R.string.video_platform_tiktok)
    Platform.INSTAGRAM -> stringResource(R.string.video_platform_instagram)
    Platform.UNKNOWN -> null
}

/** «14 МБ · без водяного знака», either part alone, or null. */
@Composable
private fun metaLine(video: Video): String? {
    val size = (video.sizeBytes ?: video.file.sizeBytes)?.let { formatFileSize(it) }
    val noWatermark = video.noWatermark == true
    return when {
        size != null && noWatermark -> stringResource(R.string.video_meta_size_no_watermark, size)
        size != null -> size
        noWatermark -> stringResource(R.string.video_no_watermark)
        else -> null
    }
}

/** «vm.tiktok.com/ZM…»: no scheme or www; the Text ellipsizes the rest. */
internal fun displayUrl(url: String): String =
    url.trim().replaceFirst(Regex("^(?i)https?://"), "").removePrefix("www.").trimEnd('/')

private fun Context.openOrToast(intent: Intent) {
    try {
        startActivity(intent)
    } catch (_: ActivityNotFoundException) {
        Toast.makeText(this, getString(R.string.video_open_failed), Toast.LENGTH_SHORT).show()
    } catch (_: SecurityException) {
        Toast.makeText(this, getString(R.string.video_open_failed), Toast.LENGTH_SHORT).show()
    }
}

// Previews: one per sheet state.

private val previewVideo = Video(
    id = "tiktok:7301234567890123456",
    platform = Platform.TIKTOK,
    sourceUrl = "https://vm.tiktok.com/ZMabcdef/",
    canonicalUrl = "https://www.tiktok.com/@author/video/7301234567890123456",
    author = "author",
    caption = "Описание видео, как его написал автор, — до трёх строк, дальше обрезается, потому что бывает очень длинным",
    durationSeconds = 27,
    sizeBytes = 14_680_064,
    width = 1080,
    height = 1920,
    noWatermark = true,
    thumbnailUrl = null,
    file = FileState(FileStatus.NONE, null),
)

@Composable
private fun PreviewSheet(state: VideoSheetUiState) {
    QuokerTheme {
        Box(Modifier.background(QuokerTheme.colors.background).width(390.dp)) {
            SheetBody(spacing = if (state.content is VideoContent.Error) 16.dp else QuokerTheme.dimens.sheetGap) {
                VideoSheetContent(state, VideoSheetActions())
            }
        }
    }
}

private val ready = VideoSheetUiState(content = VideoContent.Ready(previewVideo))

@Preview(name = "Загрузка")
@Composable
private fun LoadingPreview() = PreviewSheet(VideoSheetUiState())

@Preview(name = "Готово")
@Composable
private fun ReadyPreview() = PreviewSheet(ready)

@Preview(name = "Готовим файл · проверка Telegram")
@Composable
private fun PreparingPreview() = PreviewSheet(ready.copy(download = DownloadState.Preparing, send = SendUi.Checking))

@Preview(name = "Скачивается · отправлено")
@Composable
private fun DownloadingPreview() =
    PreviewSheet(ready.copy(download = DownloadState.Downloading(62), send = SendUi.Sent("Друзья")))

@Preview(name = "Сохранено · отправляется")
@Composable
private fun SavedPreview() =
    PreviewSheet(ready.copy(download = DownloadState.Saved("content://media/1"), send = SendUi.Sending("Друзья")))

@Preview(name = "Ошибки скачивания и отправки")
@Composable
private fun FailedPreview() = PreviewSheet(
    ready.copy(
        download = DownloadState.Failed(QuokerError(ErrorCodes.NETWORK, "", true)),
        send = SendUi.Failed("Друзья", null, sendId = 1),
    ),
)

@Preview(name = "Ошибка резолва")
@Composable
private fun ErrorPreview() = PreviewSheet(
    VideoSheetUiState(
        content = VideoContent.Error(
            QuokerError(ErrorCodes.PRIVATE_VIDEO, "", false),
            Platform.TIKTOK,
            "https://vm.tiktok.com/ZMabcdef/",
        ),
    ),
)
