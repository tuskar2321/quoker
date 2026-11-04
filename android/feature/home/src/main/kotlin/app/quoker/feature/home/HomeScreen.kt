package app.quoker.feature.home

import android.content.ClipboardManager
import android.content.Context
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalWindowInfo
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleStartEffect
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import app.quoker.core.common.format.DayBucket
import app.quoker.core.common.link.Link
import app.quoker.core.common.link.Platform as LinkPlatform
import app.quoker.core.data.model.Platform
import app.quoker.core.designsystem.component.BadgeTone
import app.quoker.core.designsystem.component.Banner
import app.quoker.core.designsystem.component.BannerTone
import app.quoker.core.designsystem.component.ButtonSize
import app.quoker.core.designsystem.component.ClipboardChip
import app.quoker.core.designsystem.component.CoverSize
import app.quoker.core.designsystem.component.FieldIconAction
import app.quoker.core.designsystem.component.NoNetworkBanner
import app.quoker.core.designsystem.component.PrimaryButton
import app.quoker.core.designsystem.component.QuokerBadge
import app.quoker.core.designsystem.component.QuokerIconButton
import app.quoker.core.designsystem.component.QuokerTextButton
import app.quoker.core.designsystem.component.QuokerTextField
import app.quoker.core.designsystem.component.TopBar
import app.quoker.core.designsystem.component.VideoCover
import app.quoker.core.designsystem.icon.QuokerIcons
import app.quoker.core.designsystem.text.errorStringRes
import app.quoker.core.designsystem.text.label
import app.quoker.core.designsystem.theme.QuokerTheme
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import app.quoker.core.designsystem.R as DesignR

@Composable
fun HomeScreen(
    onOpenVideo: (url: String) -> Unit,
    onOpenHistory: () -> Unit,
    onOpenSettings: () -> Unit,
    viewModel: HomeViewModel = hiltViewModel(),
) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    val context = LocalContext.current
    val openVideo by rememberUpdatedState(onOpenVideo)

    LifecycleStartEffect(viewModel) {
        viewModel.onStart()
        onStopOrDispose { viewModel.onStop() }
    }
    // Android 10+ gives clipboard access only to the focused app, so read it once the window has
    // focus after onResume (Android 12+ shows a system toast — expected).
    val windowFocused = LocalWindowInfo.current.isWindowFocused
    val lifecycleState by LocalLifecycleOwner.current.lifecycle.currentStateFlow.collectAsStateWithLifecycle()
    val resumed = lifecycleState.isAtLeast(Lifecycle.State.RESUMED)
    LaunchedEffect(windowFocused, resumed) {
        if (windowFocused && resumed) viewModel.onClipboardText(context.readClipboardText())
    }
    LaunchedEffect(viewModel) {
        viewModel.events.collect { event ->
            when (event) {
                is HomeEvent.OpenVideo -> openVideo(event.url)
            }
        }
    }
    HomeContent(
        state = state,
        onLinkChange = viewModel::onLinkChange,
        onPaste = { viewModel.onPaste(context.readClipboardText()) },
        onClipboardChipClick = viewModel::onClipboardChipClick,
        onOpenVideo = viewModel::onOpenVideo,
        onRecentClick = viewModel::onRecentClick,
        onOpenHistory = onOpenHistory,
        onOpenSettings = onOpenSettings,
    )
}

private fun Context.readClipboardText(): String? = try {
    val cm = getSystemService(ClipboardManager::class.java)
    cm?.primaryClip?.takeIf { it.itemCount > 0 }?.getItemAt(0)?.coerceToText(this)?.toString()
} catch (_: SecurityException) {
    null
}

@Composable
fun HomeContent(
    state: HomeUiState,
    onLinkChange: (String) -> Unit,
    onPaste: () -> Unit,
    onClipboardChipClick: () -> Unit,
    onOpenVideo: () -> Unit,
    onRecentClick: (RecentItem) -> Unit,
    onOpenHistory: () -> Unit,
    onOpenSettings: () -> Unit,
    modifier: Modifier = Modifier,
    now: Instant = Instant.now(),
) {
    val c = QuokerTheme.colors
    val gutter = QuokerTheme.dimens.screenGutter
    Column(
        modifier
            .fillMaxSize()
            .background(c.background)
            .safeDrawingPadding()
            .verticalScroll(rememberScrollState()),
    ) {
        TopBar {
            QuokerIconButton(QuokerIcons.Settings, stringResource(DesignR.string.action_settings), onOpenSettings)
        }
        if (state.offline || state.noWorkers) {
            Column(
                Modifier.padding(start = gutter, end = gutter, top = 12.dp),
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                if (state.offline) NoNetworkBanner()
                if (state.noWorkers) {
                    Banner(
                        title = stringResource(R.string.home_no_workers_title),
                        text = stringResource(R.string.home_no_workers_text),
                        tone = BannerTone.Error,
                    )
                }
            }
        }
        LinkSection(state, onLinkChange, onPaste, onClipboardChipClick, onOpenVideo)
        if (state.recent.isNotEmpty()) {
            RecentSection(state.recent, onRecentClick, onOpenHistory, now)
        }
    }
}

@Composable
private fun LinkSection(
    state: HomeUiState,
    onLinkChange: (String) -> Unit,
    onPaste: () -> Unit,
    onClipboardChipClick: () -> Unit,
    onOpenVideo: () -> Unit,
) {
    val gutter = QuokerTheme.dimens.screenGutter
    val errorRes = state.linkError?.let(::errorStringRes)
    Column(
        Modifier.padding(start = gutter, end = gutter, top = 12.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        QuokerTextField(
            value = state.link,
            onValueChange = onLinkChange,
            label = stringResource(R.string.home_link_label),
            placeholder = stringResource(R.string.home_link_placeholder),
            supportingText = errorRes?.let { stringResource(it) },
            isError = state.linkError != null,
            focusedBorderColor = QuokerTheme.colors.handle,
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, imeAction = ImeAction.Go, autoCorrectEnabled = false),
            keyboardActions = KeyboardActions(onGo = { onOpenVideo() }),
            trailing = {
                FieldIconAction(QuokerIcons.Paste, stringResource(DesignR.string.action_paste), onPaste)
            },
        )
        state.clipboardLink?.let { link ->
            ClipboardChip(
                title = stringResource(R.string.home_clipboard_paste),
                subtitle = link.displayText(),
                onClick = onClipboardChipClick,
            )
        }
        PrimaryButton(
            text = stringResource(R.string.home_open_video),
            onClick = onOpenVideo,
            size = ButtonSize.Medium,
            enabled = state.canOpen,
        )
    }
}

/** «vm.tiktok.com/ZM…»: the URL without the scheme and `www.`. */
internal fun Link.displayText(): String = url.removePrefix("https://").removePrefix("http://").removePrefix("www.")

@Composable
private fun RecentSection(
    items: List<RecentItem>,
    onRecentClick: (RecentItem) -> Unit,
    onOpenHistory: () -> Unit,
    now: Instant,
) {
    val gutter = QuokerTheme.dimens.screenGutter
    Column(Modifier.padding(start = gutter, end = gutter, top = 28.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        Row(Modifier.fillMaxWidth().padding(bottom = 8.dp), verticalAlignment = Alignment.CenterVertically) {
            Text(
                stringResource(R.string.home_recent),
                style = QuokerTheme.typography.titleSmall,
                color = QuokerTheme.colors.onSurface,
                modifier = Modifier.weight(1f).semantics { heading() },
            )
            QuokerTextButton(stringResource(R.string.home_all_history), onOpenHistory)
        }
        Column {
            items.forEachIndexed { index, item ->
                RecentRow(item, now, onClick = { onRecentClick(item) })
                if (index != items.lastIndex) HorizontalDivider(thickness = 1.dp, color = QuokerTheme.colors.divider)
            }
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun RecentRow(item: RecentItem, now: Instant, onClick: () -> Unit) {
    val c = QuokerTheme.colors
    val t = QuokerTheme.typography
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(role = Role.Button, onClick = onClick)
            .padding(vertical = 10.dp),
        horizontalArrangement = Arrangement.spacedBy(14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        VideoCover(model = item.coverUrl, size = CoverSize.Recent)
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            val platform = stringResource(
                when (item.platform) {
                    Platform.TIKTOK -> R.string.home_platform_tiktok
                    Platform.INSTAGRAM -> R.string.home_platform_reels
                    Platform.UNKNOWN -> R.string.home_platform_unknown
                },
            )
            Text(
                item.author?.let { stringResource(R.string.home_recent_title, it, platform) } ?: platform,
                style = t.bodyLarge,
                color = c.onSurface,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            val hasBadges = item.downloaded || item.sentTo.isNotEmpty() || item.sendFailed
            if (hasBadges) {
                FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                    if (item.downloaded) QuokerBadge(stringResource(R.string.home_badge_downloaded), BadgeTone.Download)
                    item.sentTo.forEach { QuokerBadge(stringResource(R.string.home_badge_sent, it), BadgeTone.Send) }
                    if (item.sendFailed) QuokerBadge(stringResource(R.string.home_badge_send_failed), BadgeTone.Error)
                }
            }
        }
        item.at?.let { at ->
            Text(recentTimeLabel(at, now), style = t.labelSmall.copy(fontWeight = FontWeight.W500), color = c.onSurfaceVariant)
        }
    }
}

private val timeFormat = DateTimeFormatter.ofPattern("HH:mm")

/** «12:40» today, «Вчера», then the date. */
@Composable
private fun recentTimeLabel(at: Instant, now: Instant, zone: ZoneId = ZoneId.systemDefault()): String =
    when (val bucket = DayBucket.of(at, now, zone)) {
        DayBucket.Today -> timeFormat.format(at.atZone(zone))
        else -> bucket.label()
    }

// Previews

private val previewNow: Instant = Instant.parse("2026-11-04T12:00:00Z")

private val previewRecent = listOf(
    RecentItem(1, "https://www.tiktok.com/@a/video/1", null, "author", Platform.TIKTOK, true, listOf("Друзья"), false, previewNow.minusSeconds(3600)),
    RecentItem(2, "https://www.instagram.com/reel/x", null, "author", Platform.INSTAGRAM, false, listOf("Мой канал"), false, previewNow.minusSeconds(90_000)),
    RecentItem(3, "https://www.tiktok.com/@a/video/3", null, "author", Platform.TIKTOK, true, emptyList(), true, previewNow.minusSeconds(100_000)),
)

private val previewClipboard = Link(LinkPlatform.TikTok, null, "tiktok:short:ZMabc", "https://vm.tiktok.com/ZMabc123/")

@Composable
private fun PreviewHome(state: HomeUiState) {
    QuokerTheme { HomeContent(state, {}, {}, {}, {}, {}, {}, {}, now = previewNow) }
}

@Preview(widthDp = 390, heightDp = 844)
@Composable
private fun HomeEmptyPreview() = PreviewHome(HomeUiState())

@Preview(widthDp = 390, heightDp = 844)
@Composable
private fun HomeClipboardAndHistoryPreview() =
    PreviewHome(HomeUiState(clipboardLink = previewClipboard, recent = previewRecent))

@Preview(widthDp = 390, heightDp = 844)
@Composable
private fun HomeLinkErrorPreview() =
    PreviewHome(HomeUiState(link = "https://www.instagram.com/stories/x/1", linkError = "unsupported_content", recent = previewRecent))

@Preview(widthDp = 390, heightDp = 844)
@Composable
private fun HomeOfflineAdminPreview() =
    PreviewHome(HomeUiState(offline = true, noWorkers = true, recent = previewRecent.take(1)))
