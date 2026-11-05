package app.quoker.feature.history

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyListState
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import app.quoker.core.common.QuokerError
import app.quoker.core.common.format.DayBucket
import app.quoker.core.common.format.formatDuration
import app.quoker.core.data.model.Platform
import app.quoker.core.designsystem.component.BadgeTone
import app.quoker.core.designsystem.component.Banner
import app.quoker.core.designsystem.component.BannerTone
import app.quoker.core.designsystem.component.ButtonSize
import app.quoker.core.designsystem.component.CoverSize
import app.quoker.core.designsystem.component.EmptyState
import app.quoker.core.designsystem.component.NoNetworkBanner
import app.quoker.core.designsystem.component.QuokerBadge
import app.quoker.core.designsystem.component.ScreenHeader
import app.quoker.core.designsystem.component.SecondaryButton
import app.quoker.core.designsystem.component.SectionHeader
import app.quoker.core.designsystem.component.SkeletonBlock
import app.quoker.core.designsystem.component.SkeletonLine
import app.quoker.core.designsystem.component.VideoCover
import app.quoker.core.designsystem.text.formatFileSize
import app.quoker.core.designsystem.text.label
import app.quoker.core.designsystem.text.userMessage
import app.quoker.core.designsystem.theme.QuokerTheme
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.filter
import app.quoker.core.designsystem.R as DsR

private const val LOAD_MORE_THRESHOLD = 5
private const val MAX_SENT_BADGES = 2
private val timeFormat = DateTimeFormatter.ofPattern("HH:mm")

@Composable
fun HistoryScreen(
    onBack: () -> Unit,
    onOpenVideo: (String) -> Unit,
    onDownload: (String) -> Unit,
    onSend: (videoId: String, retrySendId: Long?) -> Unit,
    viewModel: HistoryViewModel = hiltViewModel(),
) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    HistoryContent(
        state = state,
        onBack = onBack,
        onOpenVideo = onOpenVideo,
        onDownload = onDownload,
        onSend = onSend,
        onRetry = viewModel::refresh,
        onLoadMore = viewModel::loadMore,
        onRetryLoadMore = viewModel::retryLoadMore,
    )
}

@Composable
internal fun HistoryContent(
    state: HistoryUiState,
    onBack: () -> Unit,
    onOpenVideo: (String) -> Unit,
    onDownload: (String) -> Unit,
    onSend: (videoId: String, retrySendId: Long?) -> Unit,
    onRetry: () -> Unit,
    onLoadMore: () -> Unit,
    onRetryLoadMore: () -> Unit,
    zone: ZoneId = ZoneId.systemDefault(),
) {
    val gutter = QuokerTheme.dimens.screenGutter
    Column(
        Modifier
            .fillMaxSize()
            .background(QuokerTheme.colors.background)
            .statusBarsPadding(),
    ) {
        ScreenHeader(title = stringResource(R.string.history_title), onBack = onBack)
        when {
            state.showSkeleton -> HistorySkeleton(Modifier.padding(horizontal = gutter))
            state.showError -> EmptyState(
                title = stringResource(R.string.history_error_title),
                text = state.error!!.userMessage(),
                modifier = Modifier.padding(horizontal = gutter, vertical = 32.dp),
                actionText = stringResource(DsR.string.action_retry),
                onAction = onRetry,
            )
            state.showEmpty -> EmptyState(
                title = stringResource(R.string.history_empty_title),
                text = stringResource(R.string.history_empty_text),
                modifier = Modifier.padding(horizontal = gutter, vertical = 32.dp),
            )
            else -> HistoryList(state, onOpenVideo, onDownload, onSend, onRetry, onLoadMore, onRetryLoadMore, zone)
        }
    }
}

@Composable
private fun HistoryList(
    state: HistoryUiState,
    onOpenVideo: (String) -> Unit,
    onDownload: (String) -> Unit,
    onSend: (videoId: String, retrySendId: Long?) -> Unit,
    onRetry: () -> Unit,
    onLoadMore: () -> Unit,
    onRetryLoadMore: () -> Unit,
    zone: ZoneId,
    listState: LazyListState = rememberLazyListState(),
) {
    val loadMore by rememberUpdatedState(onLoadMore)
    // Re-armed when the list or the loading state changes, so a short list keeps paging.
    LaunchedEffect(listState, state.sections, state.refreshing, state.loadingMore, state.endReached) {
        if (state.endReached) return@LaunchedEffect
        snapshotFlowNearEnd(listState).collect { loadMore() }
    }
    val gutter = QuokerTheme.dimens.screenGutter
    val lastCardId = state.sections.lastOrNull()?.cards?.lastOrNull()?.id
    LazyColumn(
        state = listState,
        modifier = Modifier.fillMaxSize(),
        contentPadding = PaddingValues(start = gutter, end = gutter, top = 8.dp, bottom = 24.dp),
    ) {
        if (state.offline) {
            item(key = "offline") { NoNetworkBanner(Modifier.padding(bottom = 8.dp), onRetry = onRetry) }
        }
        state.bannerError?.let { error ->
            item(key = "error") {
                Banner(
                    text = error.userMessage(),
                    modifier = Modifier.padding(bottom = 8.dp),
                    tone = BannerTone.Error,
                    actionText = stringResource(DsR.string.action_retry),
                    onAction = onRetry,
                )
            }
        }
        state.sections.forEachIndexed { index, section ->
            item(key = "h:$index:${section.bucket}", contentType = "header") {
                SectionHeader(
                    section.bucket.label(),
                    Modifier.padding(top = if (index == 0) 8.dp else 20.dp, bottom = 4.dp),
                )
            }
            section.cards.forEach { card ->
                item(key = card.id, contentType = "card") {
                    HistoryCardRow(
                        card = card,
                        zone = zone,
                        divider = card.id != lastCardId,
                        onOpen = { onOpenVideo(card.videoId) },
                        onDownload = { onDownload(card.videoId) },
                        onSend = { onSend(card.videoId, card.retrySendId) },
                    )
                }
            }
        }
        when {
            state.loadingMore -> item(key = "more") {
                Box(Modifier.fillMaxWidth().padding(vertical = 16.dp), contentAlignment = Alignment.Center) {
                    CircularProgressIndicator(
                        modifier = Modifier.size(24.dp),
                        color = QuokerTheme.colors.onSurfaceVariant,
                        strokeWidth = 2.dp,
                    )
                }
            }
            state.loadMoreError != null -> item(key = "more-error") {
                Column(
                    Modifier.fillMaxWidth().padding(vertical = 16.dp),
                    horizontalAlignment = Alignment.CenterHorizontally,
                    verticalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    Text(
                        stringResource(R.string.history_load_more_error),
                        style = QuokerTheme.typography.bodySmall,
                        color = QuokerTheme.colors.onSurfaceVariant,
                    )
                    SecondaryButton(stringResource(DsR.string.action_retry), onRetryLoadMore, size = ButtonSize.Mini)
                }
            }
        }
    }
}

private fun snapshotFlowNearEnd(listState: LazyListState) =
    snapshotFlow {
        val info = listState.layoutInfo
        val last = info.visibleItemsInfo.lastOrNull()?.index ?: -1
        info.totalItemsCount > 0 && last >= info.totalItemsCount - LOAD_MORE_THRESHOLD
    }.distinctUntilChanged().filter { it }

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun HistoryCardRow(
    card: HistoryCard,
    zone: ZoneId,
    divider: Boolean,
    onOpen: () -> Unit,
    onDownload: () -> Unit,
    onSend: () -> Unit,
) {
    val c = QuokerTheme.colors
    val t = QuokerTheme.typography
    Column(Modifier.fillMaxWidth()) {
        Row(
            modifier = Modifier.fillMaxWidth().clickable(onClick = onOpen).padding(vertical = 14.dp),
            horizontalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            VideoCover(model = card.thumbnailUrl, size = CoverSize.History)
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Text(
                        text = card.author?.let { stringResource(R.string.history_author, it.removePrefix("@")) }
                            ?: stringResource(R.string.history_author_unknown),
                        style = t.bodyLarge.copy(fontWeight = FontWeight.W700),
                        color = c.onSurface,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.weight(1f),
                    )
                    card.lastActionAt?.let {
                        Text(
                            timeFormat.format(it.atZone(zone)),
                            style = t.labelSmall.copy(fontWeight = FontWeight.W500),
                            color = c.onSurfaceVariant,
                        )
                    }
                }
                val meta = metaLine(card)
                if (meta.isNotEmpty()) Text(meta, style = t.bodySmall, color = c.onSurfaceVariant)
                if (card.downloaded || card.sentTo.isNotEmpty() || card.sendFailed) {
                    FlowRow(
                        horizontalArrangement = Arrangement.spacedBy(6.dp),
                        verticalArrangement = Arrangement.spacedBy(6.dp),
                    ) {
                        if (card.downloaded) QuokerBadge(stringResource(R.string.history_badge_downloaded), BadgeTone.Download)
                        card.sentTo.take(MAX_SENT_BADGES).forEach {
                            QuokerBadge(stringResource(R.string.history_badge_sent, it), BadgeTone.Send)
                        }
                        val rest = card.sentTo.size - MAX_SENT_BADGES
                        if (rest > 0) QuokerBadge(pluralStringResource(R.plurals.history_badge_sent_more, rest, rest), BadgeTone.Neutral)
                        if (card.sendFailed) QuokerBadge(stringResource(R.string.history_badge_send_failed), BadgeTone.Error)
                    }
                }
                Row(Modifier.padding(top = 2.dp), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    SecondaryButton(
                        text = stringResource(if (card.downloaded) R.string.history_action_download_again else R.string.history_action_download),
                        onClick = onDownload,
                        size = ButtonSize.Mini,
                    )
                    SecondaryButton(
                        text = stringResource(if (card.sendFailed) R.string.history_action_retry_send else R.string.history_action_send),
                        onClick = onSend,
                        size = ButtonSize.Mini,
                    )
                }
            }
        }
        if (divider) HorizontalDivider(thickness = 1.dp, color = c.divider)
    }
}

/** «TikTok · 0:27 · 14 МБ», skipping unknown parts. */
@Composable
private fun metaLine(card: HistoryCard): String {
    val parts = buildList {
        when (card.platform) {
            Platform.TIKTOK -> add(stringResource(R.string.history_platform_tiktok))
            Platform.INSTAGRAM -> add(stringResource(R.string.history_platform_instagram))
            Platform.UNKNOWN -> Unit
        }
        card.durationSeconds?.let { add(formatDuration(it)) }
        card.sizeBytes?.let { add(formatFileSize(it)) }
    }
    return parts.joinToString(stringResource(R.string.history_meta_separator))
}

@Composable
private fun HistorySkeleton(modifier: Modifier = Modifier) {
    Column(modifier.padding(top = 16.dp)) {
        SkeletonLine(widthFraction = 0.25f, height = 12.dp)
        repeat(4) {
            Row(Modifier.padding(vertical = 14.dp), horizontalArrangement = Arrangement.spacedBy(14.dp)) {
                SkeletonBlock(
                    Modifier.size(QuokerTheme.dimens.coverHistory),
                    RoundedCornerShape(QuokerTheme.dimens.coverHistoryRadius),
                )
                Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    SkeletonLine(widthFraction = 0.5f, height = 16.dp)
                    SkeletonLine(widthFraction = 0.7f, height = 12.dp)
                    SkeletonLine(widthFraction = 0.4f, height = 20.dp)
                }
            }
        }
    }
}

// Previews

private val previewNow = Instant.parse("2026-11-04T15:00:00Z")
private val previewZone = ZoneId.of("UTC")

private fun previewCard(
    id: Long,
    platform: Platform,
    hour: Int,
    daysAgo: Long = 0,
    downloaded: Boolean = false,
    sentTo: List<String> = emptyList(),
    failed: Boolean = false,
) = HistoryCard(
    id = id,
    videoId = "v$id",
    author = "author",
    platform = platform,
    durationSeconds = 27,
    sizeBytes = 14_680_064,
    thumbnailUrl = null,
    lastActionAt = previewNow.minusSeconds(daysAgo * 86_400).atZone(previewZone).withHour(hour).withMinute(40).toInstant(),
    downloaded = downloaded,
    sentTo = sentTo,
    sendFailed = failed,
    retrySendId = if (failed) 1 else null,
)

private val previewSections = listOf(
    HistorySection(
        DayBucket.Today,
        listOf(
            previewCard(1, Platform.TIKTOK, 12, downloaded = true, sentTo = listOf("Друзья")),
            previewCard(2, Platform.INSTAGRAM, 10, failed = true),
        ),
    ),
    HistorySection(DayBucket.Yesterday, listOf(previewCard(3, Platform.INSTAGRAM, 21, daysAgo = 1, sentTo = listOf("Мой канал")))),
    HistorySection(
        DayBucket.Date(LocalDate.of(2026, 10, 30), sameYear = true),
        listOf(previewCard(4, Platform.TIKTOK, 9, daysAgo = 5, sentTo = listOf("Друзья", "Мой канал", "Работа", "Семья"))),
    ),
)

@Composable
private fun Preview(state: HistoryUiState) {
    QuokerTheme {
        HistoryContent(state, {}, {}, {}, { _, _ -> }, {}, {}, {}, zone = previewZone)
    }
}

@Preview(name = "Content", widthDp = 390, heightDp = 844)
@Composable
private fun HistoryContentPreview() = Preview(HistoryUiState(sections = previewSections, loaded = true, endReached = false))

@Preview(name = "Offline", widthDp = 390, heightDp = 844)
@Composable
private fun HistoryOfflinePreview() = Preview(
    HistoryUiState(sections = previewSections, loaded = true, error = QuokerError("network", "", retryable = true)),
)

@Preview(name = "Loading more", widthDp = 390, heightDp = 844)
@Composable
private fun HistoryLoadingMorePreview() =
    Preview(HistoryUiState(sections = previewSections.take(1), loaded = true, loadingMore = true, endReached = false))

@Preview(name = "Load more error", widthDp = 390, heightDp = 844)
@Composable
private fun HistoryLoadMoreErrorPreview() = Preview(
    HistoryUiState(
        sections = previewSections.take(1),
        loaded = true,
        endReached = false,
        loadMoreError = QuokerError("internal", "", retryable = true),
    ),
)

@Preview(name = "Skeleton", widthDp = 390, heightDp = 844)
@Composable
private fun HistorySkeletonPreview() = Preview(HistoryUiState(refreshing = true))

@Preview(name = "Empty", widthDp = 390, heightDp = 844)
@Composable
private fun HistoryEmptyPreview() = Preview(HistoryUiState(loaded = true))

@Preview(name = "Error", widthDp = 390, heightDp = 844)
@Composable
private fun HistoryErrorPreview() =
    Preview(HistoryUiState(error = QuokerError("network", "", retryable = true)))
