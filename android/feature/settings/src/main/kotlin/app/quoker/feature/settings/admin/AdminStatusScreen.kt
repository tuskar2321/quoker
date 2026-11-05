package app.quoker.feature.settings.admin

import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import app.quoker.core.common.QuokerError
import app.quoker.core.data.model.AdminStatus
import app.quoker.core.data.model.DiskUsage
import app.quoker.core.data.model.PlatformErrors
import app.quoker.core.data.model.Worker
import app.quoker.core.data.model.WorkerStatus
import app.quoker.core.designsystem.component.Banner
import app.quoker.core.designsystem.component.BannerTone
import app.quoker.core.designsystem.component.ButtonSize
import app.quoker.core.designsystem.component.ListGroup
import app.quoker.core.designsystem.component.ListRow
import app.quoker.core.designsystem.component.ListRowChevron
import app.quoker.core.designsystem.component.PrimaryButton
import app.quoker.core.designsystem.component.QuokerProgressBar
import app.quoker.core.designsystem.component.RowTextTone
import app.quoker.core.designsystem.component.SectionHeader
import app.quoker.core.designsystem.component.SkeletonBlock
import app.quoker.core.designsystem.component.StatusDot
import app.quoker.core.designsystem.text.formatFileSize
import app.quoker.core.designsystem.theme.QuokerTheme
import app.quoker.feature.settings.R
import app.quoker.feature.settings.ui.LoadError
import app.quoker.feature.settings.ui.SettingsScaffold
import app.quoker.feature.settings.ui.UiMessage
import java.time.Instant

@Composable
internal fun AdminStatusScreen(
    onBack: () -> Unit,
    onOpenInvites: () -> Unit,
    onOpenUsers: () -> Unit,
    onOpenWorkers: () -> Unit,
    viewModel: AdminStatusViewModel = hiltViewModel(),
) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { viewModel.onResume() }
    val picker = rememberLauncherForActivityResult(ActivityResultContracts.OpenDocument()) { uri ->
        if (uri != null) viewModel.onCookiesFilePicked(uri.toString())
    }
    AdminStatusContent(
        state = state,
        actions = AdminStatusActions(
            onBack = onBack,
            onRetry = viewModel::retry,
            onOpenInvites = onOpenInvites,
            onOpenUsers = onOpenUsers,
            onOpenWorkers = onOpenWorkers,
            onUploadCookies = { picker.launch(arrayOf("text/*")) },
            onDismissMessage = viewModel::dismissMessage,
        ),
    )
}

internal data class AdminStatusActions(
    val onBack: () -> Unit = {},
    val onRetry: () -> Unit = {},
    val onOpenInvites: () -> Unit = {},
    val onOpenUsers: () -> Unit = {},
    val onOpenWorkers: () -> Unit = {},
    val onUploadCookies: () -> Unit = {},
    val onDismissMessage: () -> Unit = {},
)

@Composable
internal fun AdminStatusContent(state: AdminStatusUiState, actions: AdminStatusActions) {
    SettingsScaffold(
        title = stringResource(R.string.admin_title),
        onBack = actions.onBack,
        message = state.message,
        onDismissMessage = actions.onDismissMessage,
        spacing = 12.dp,
    ) {
        state.alerts.forEach { AlertCard(it) }
        val status = state.status
        when {
            status != null -> StateGrid(state, status)
            state.error != null -> LoadError(state.error, actions.onRetry)
            else -> {
                SectionHeader(stringResource(R.string.admin_section_state), Modifier.padding(top = 8.dp))
                repeat(2) {
                    Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                        repeat(2) { SkeletonBlock(Modifier.weight(1f).height(96.dp), RoundedCornerShape(16.dp)) }
                    }
                }
            }
        }
        ManageGroup(state, actions, Modifier.padding(top = 8.dp))
    }
}

@Composable
private fun AlertCard(alert: AdminAlert) {
    when (alert) {
        AdminAlert.NoWorkers -> Banner(
            title = stringResource(R.string.admin_alert_no_workers_title),
            text = stringResource(R.string.admin_alert_no_workers_text),
            tone = BannerTone.Error,
        )
        is AdminAlert.InstagramErrors -> Banner(
            title = stringResource(R.string.admin_alert_instagram_title),
            text = pluralStringResource(R.plurals.admin_alert_instagram_text, alert.count, alert.count),
            tone = BannerTone.Error,
        )
        is AdminAlert.DiskAlmostFull -> Banner(
            title = stringResource(R.string.admin_alert_disk_title, alert.percent),
            text = stringResource(R.string.admin_alert_disk_text, formatFileSize(alert.cacheBytes)),
            tone = BannerTone.Error,
        )
    }
}

@Composable
private fun StateGrid(state: AdminStatusUiState, status: AdminStatus) {
    val c = QuokerTheme.colors
    SectionHeader(stringResource(R.string.admin_section_state), Modifier.padding(top = 8.dp))
    Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
        Row(Modifier.height(IntrinsicSize.Min), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            val online = state.onlineWorkers
            Tile(stringResource(R.string.admin_tile_workers), Modifier.weight(1f)) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    StatusDot(active = online.isNotEmpty())
                    TileValue(stringResource(R.string.admin_tile_workers_online, online.size))
                }
                TileCaption(
                    state.poller?.let { stringResource(R.string.admin_tile_workers_poller, it.id) }
                        ?: stringResource(R.string.admin_tile_workers_no_poller),
                )
            }
            Tile(stringResource(R.string.admin_tile_queue), Modifier.weight(1f)) {
                TileValue(stringResource(R.string.admin_tile_queue_running, status.running))
                TileCaption(pluralStringResource(R.plurals.admin_tile_queue_waiting, status.queued, status.queued))
            }
        }
        Row(Modifier.height(IntrinsicSize.Min), horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            val e = status.errors24h
            Tile(stringResource(R.string.admin_tile_errors), Modifier.weight(1f)) {
                TileValue((e.tiktok + e.instagram + e.telegram).toString())
                TileCaption(stringResource(R.string.admin_tile_errors_by_platform, e.tiktok, e.instagram, e.telegram))
            }
            val percent = AdminStatusUiState.diskPercent(status)
            Tile(stringResource(R.string.admin_tile_disk), Modifier.weight(1f)) {
                TileValue(stringResource(R.string.admin_tile_disk_percent, percent))
                QuokerProgressBar(
                    status.disk.usedFraction,
                    color = if (percent > AdminStatusUiState.DISK_ALERT_PERCENT) c.error else c.onSurface,
                )
                TileCaption(
                    stringResource(
                        R.string.admin_tile_disk_cache,
                        formatFileSize(status.disk.cacheBytes),
                        formatFileSize(status.disk.totalBytes),
                    ),
                )
            }
        }
    }
}

@Composable
private fun Tile(label: String, modifier: Modifier = Modifier, content: @Composable ColumnScope.() -> Unit) {
    Column(
        modifier
            .fillMaxHeight()
            .background(QuokerTheme.colors.surface, RoundedCornerShape(QuokerTheme.dimens.cardRadius))
            .padding(horizontal = 16.dp, vertical = 14.dp),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        Text(label, style = QuokerTheme.typography.labelSmall, color = QuokerTheme.colors.onSurfaceVariant)
        content()
    }
}

@Composable
private fun TileValue(text: String) {
    Text(
        text,
        style = QuokerTheme.typography.titleMedium.copy(fontWeight = FontWeight.W800, fontSize = 20.sp),
        color = QuokerTheme.colors.onSurface,
    )
}

@Composable
private fun TileCaption(text: String) {
    Text(
        text,
        style = QuokerTheme.typography.labelSmall.copy(fontWeight = FontWeight.W500),
        color = QuokerTheme.colors.onSurfaceVariant,
    )
}

@Composable
private fun ManageGroup(state: AdminStatusUiState, actions: AdminStatusActions, modifier: Modifier = Modifier) {
    ListGroup(modifier = modifier, header = stringResource(R.string.admin_section_manage)) {
        ListRow(
            title = stringResource(R.string.admin_invites),
            subtitle = state.activeInvites?.let {
                if (it == 0) stringResource(R.string.admin_invites_none) else pluralStringResource(R.plurals.admin_invites_active, it, it)
            },
            trailing = { ListRowChevron() },
            onClick = actions.onOpenInvites,
            divider = true,
        )
        ListRow(
            title = stringResource(R.string.admin_users),
            subtitle = state.users?.let { u ->
                if (u.blocked == 0) {
                    u.total.toString()
                } else {
                    stringResource(
                        R.string.admin_users_summary,
                        u.total,
                        pluralStringResource(R.plurals.admin_users_blocked_short, u.blocked, u.blocked),
                    )
                }
            },
            trailing = { ListRowChevron() },
            onClick = actions.onOpenUsers,
            divider = true,
        )
        val age = state.cookiesAgeDays
        ListRow(
            title = stringResource(R.string.admin_cookies),
            subtitle = when (age) {
                null -> stringResource(R.string.admin_cookies_unknown)
                0L -> stringResource(R.string.admin_cookies_today)
                else -> pluralStringResource(R.plurals.admin_cookies_days_ago, age.toInt(), age.toInt())
            },
            subtitleTone = if (state.cookiesStale) RowTextTone.Error else RowTextTone.Muted,
            divider = true,
            trailing = {
                PrimaryButton(
                    text = stringResource(R.string.admin_cookies_upload),
                    onClick = actions.onUploadCookies,
                    size = ButtonSize.Compact,
                    loading = state.uploadingCookies,
                )
            },
        )
        val ytdlp = state.onlineWorkers.firstNotNullOfOrNull { it.ytdlpVersion }
        ListRow(
            title = stringResource(R.string.admin_workers),
            subtitle = when {
                state.status == null -> null
                ytdlp != null -> stringResource(R.string.admin_workers_ytdlp, ytdlp)
                state.onlineWorkers.isEmpty() -> stringResource(R.string.admin_workers_none_online)
                else -> null
            },
            trailing = { ListRowChevron() },
            onClick = actions.onOpenWorkers,
        )
    }
}

// Previews

internal fun previewWorker(id: String, connected: Boolean = true, poller: Boolean = false, revoked: Boolean = false) = Worker(
    id = id,
    fingerprint = "ab".repeat(32),
    status = if (revoked) WorkerStatus.REVOKED else WorkerStatus.ALLOWED,
    connected = connected,
    version = "1.4.0",
    ytdlpVersion = "2026.10.22",
    certNotAfter = Instant.parse("2027-11-12T00:00:00Z"),
    lastSeenAt = Instant.parse("2026-11-05T09:40:00Z"),
    isPoller = poller,
)

private fun previewStatus(workers: List<Worker>, instagram: Int = 0, used: Long = 1_300_000_000L) = AdminStatus(
    workers = workers,
    queued = 1,
    running = 2,
    errors24h = PlatformErrors(tiktok = 0, instagram = instagram, telegram = 0),
    disk = DiskUsage(usedBytes = used, totalBytes = 3_000_000_000L, cacheBytes = 1_200_000_000L),
    coreVersion = "1.0.0",
    appLatestVersion = "1.0.0",
)

private val previewNow = Instant.parse("2026-11-05T12:00:00Z")

@Preview(widthDp = 390, heightDp = 844)
@Composable
private fun AdminStatusAlertPreview() {
    QuokerTheme {
        AdminStatusContent(
            AdminStatusUiState(
                status = previewStatus(listOf(previewWorker("worker-1", poller = true)), instagram = 3),
                activeInvites = 2,
                users = UsersSummary(5, 1),
                cookiesUploadedAt = previewNow.minusSeconds(21L * 24 * 3600),
                now = previewNow,
            ),
            AdminStatusActions(),
        )
    }
}

@Preview(widthDp = 390, heightDp = 844)
@Composable
private fun AdminStatusNoWorkersPreview() {
    QuokerTheme {
        AdminStatusContent(
            AdminStatusUiState(
                status = previewStatus(listOf(previewWorker("worker-1", connected = false)), used = 2_700_000_000L),
                activeInvites = 0,
                users = UsersSummary(5, 0),
                now = previewNow,
                uploadingCookies = true,
            ),
            AdminStatusActions(),
        )
    }
}

@Preview(widthDp = 390, heightDp = 844)
@Composable
private fun AdminStatusLoadingPreview() {
    QuokerTheme { AdminStatusContent(AdminStatusUiState(now = previewNow), AdminStatusActions()) }
}

@Preview(widthDp = 390, heightDp = 844)
@Composable
private fun AdminStatusErrorPreview() {
    QuokerTheme {
        AdminStatusContent(
            AdminStatusUiState(error = QuokerError("network", "", true), message = UiMessage.Info(R.string.admin_cookies_uploaded), now = previewNow),
            AdminStatusActions(),
        )
    }
}
