package app.quoker.feature.send

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import app.quoker.core.common.QuokerError
import app.quoker.core.data.model.InactiveReason
import app.quoker.core.data.model.Target
import app.quoker.core.data.model.TargetStatus
import app.quoker.core.data.model.TargetType
import app.quoker.core.designsystem.component.AvatarTone
import app.quoker.core.designsystem.component.Banner
import app.quoker.core.designsystem.component.BannerTone
import app.quoker.core.designsystem.component.ButtonSize
import app.quoker.core.designsystem.component.InitialAvatar
import app.quoker.core.designsystem.component.PrimaryButton
import app.quoker.core.designsystem.component.PrimaryButtonVariant
import app.quoker.core.designsystem.component.QuokerBottomSheet
import app.quoker.core.designsystem.component.QuokerCheckbox
import app.quoker.core.designsystem.component.QuokerIconButton
import app.quoker.core.designsystem.component.QuokerTextArea
import app.quoker.core.designsystem.component.SecondaryButton
import app.quoker.core.designsystem.component.SelectableRow
import app.quoker.core.designsystem.component.SheetBody
import app.quoker.core.designsystem.component.SkeletonBlock
import app.quoker.core.designsystem.icon.QuokerIcons
import app.quoker.core.designsystem.text.userMessage
import app.quoker.core.designsystem.theme.QuokerTheme
import java.time.Instant
import java.util.UUID
import app.quoker.core.designsystem.R as DsR

/**
 * The "Куда отправить" sheet for [videoId]. [onSent] fires after the core accepted the send (it is
 * registered in SendTracker, the video sheet shows its progress); [onLinkTelegram] when the user
 * has no linked Telegram; [onDismiss] on back or swipe.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SendSheet(
    videoId: String,
    onSent: () -> Unit,
    onLinkTelegram: () -> Unit,
    onDismiss: () -> Unit,
) {
    val viewModel: SendViewModel = hiltViewModel(key = "send:$videoId")
    // A new id per opening of the sheet; it survives recreation so the Idempotency-Key does too.
    val sessionId = rememberSaveable(videoId) { UUID.randomUUID().toString() }
    DisposableEffect(viewModel, videoId, sessionId) {
        viewModel.open(videoId, sessionId)
        onDispose { viewModel.close() }
    }
    val sent by rememberUpdatedState(onSent)
    val link by rememberUpdatedState(onLinkTelegram)
    LaunchedEffect(viewModel) {
        viewModel.events.collect { event ->
            when (event) {
                SendEvent.Sent -> sent()
                SendEvent.LinkTelegram -> link()
            }
        }
    }
    val state by viewModel.state.collectAsStateWithLifecycle()
    val shown = if (state.sessionId == sessionId) state else SendUiState(sessionId = sessionId)

    QuokerBottomSheet(onDismissRequest = onDismiss, spacing = 16.dp) {
        SendSheetContent(
            state = shown,
            onBack = onDismiss,
            onSelect = viewModel::select,
            onCaptionChange = viewModel::onCaptionChange,
            onSignAsUserChange = viewModel::onSignAsUserChange,
            onSend = viewModel::send,
            onRefreshList = viewModel::refreshList,
            onRetryFetch = viewModel::retryFetch,
        )
    }
}

@Composable
internal fun ColumnScope.SendSheetContent(
    state: SendUiState,
    onBack: () -> Unit,
    onSelect: (Long) -> Unit,
    onCaptionChange: (String) -> Unit,
    onSignAsUserChange: (Boolean) -> Unit,
    onSend: () -> Unit,
    onRefreshList: () -> Unit,
    onRetryFetch: () -> Unit,
) {
    Row(
        modifier = Modifier.offset(x = (-12).dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        QuokerIconButton(QuokerIcons.Back, stringResource(DsR.string.action_back), onBack)
        Text(stringResource(R.string.send_title), style = QuokerTheme.typography.titleMedium, color = QuokerTheme.colors.onSurface)
    }

    when {
        state.loading -> TargetsSkeleton()
        state.loadFailed -> Banner(
            text = state.fetchError!!.userMessage(),
            title = stringResource(R.string.send_load_error_title),
            tone = BannerTone.Error,
            actionText = stringResource(DsR.string.action_retry),
            onAction = onRetryFetch,
        )
        else -> {
            val scroll = rememberScrollState()
            Column(
                modifier = Modifier
                    .weight(1f, fill = false)
                    .verticalScroll(scroll),
                verticalArrangement = Arrangement.spacedBy(16.dp),
            ) {
                if (state.empty) EmptyTargets(refreshing = state.refreshing, onRefresh = onRefreshList)
                if (state.targets.isNotEmpty()) {
                    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        state.targets.forEach { item ->
                            TargetRow(item, selected = item.id == state.selected?.id, onClick = { onSelect(item.id) })
                        }
                    }
                }
                if (state.hasActiveTargets) {
                    CaptionField(state, onCaptionChange)
                    if (state.showSignToggle) SignToggle(state, onSignAsUserChange)
                }
            }
            if (!state.empty && state.fetchError != null) {
                Banner(text = state.fetchError.userMessage(), tone = BannerTone.Error)
            }
            state.sendError?.let { Banner(text = it.userMessage(), tone = BannerTone.Error) }
            if (state.hasActiveTargets) {
                val title = state.selected?.target?.title
                PrimaryButton(
                    text = if (title != null) stringResource(R.string.send_button, title) else stringResource(R.string.send_button_no_target),
                    onClick = onSend,
                    variant = PrimaryButtonVariant.Send,
                    leadingIcon = QuokerIcons.Send,
                    enabled = state.selected != null,
                    loading = state.sending,
                )
            }
        }
    }
}

@Composable
private fun TargetsSkeleton() {
    Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
        repeat(3) {
            SkeletonBlock(Modifier.fillMaxWidth().height(64.dp), RoundedCornerShape(QuokerTheme.dimens.cardRadius))
        }
    }
}

@Composable
private fun TargetRow(item: TargetItem, selected: Boolean, onClick: () -> Unit) {
    val t = item.target
    val subtitle = if (item.enabled) {
        val type = targetTypeLabel(t.type)
        if (item.isLastUsed) stringResource(R.string.send_target_last_used, type) else type
    } else {
        inactiveReasonLabel(t.inactiveReason, t.type)
    }
    SelectableRow(
        selected = selected,
        onClick = onClick,
        title = t.title,
        subtitle = subtitle,
        enabled = item.enabled,
        leading = {
            if (t.type == TargetType.PRIVATE) {
                InitialAvatar("", tone = if (selected) AvatarTone.SendSolid else AvatarTone.Neutral, icon = QuokerIcons.Bookmark)
            } else {
                InitialAvatar(t.title, tone = if (selected) AvatarTone.SendSolid else AvatarTone.Neutral)
            }
        },
    )
}

@Composable
private fun targetTypeLabel(type: TargetType): String = stringResource(
    when (type) {
        TargetType.GROUP -> R.string.send_target_type_group
        TargetType.CHANNEL -> R.string.send_target_type_channel
        TargetType.PRIVATE -> R.string.send_target_type_private
        TargetType.UNKNOWN -> R.string.send_target_type_unknown
    },
)

@Composable
private fun inactiveReasonLabel(reason: InactiveReason?, type: TargetType): String = stringResource(
    when (reason) {
        InactiveReason.BOT_REMOVED -> when (type) {
            TargetType.GROUP -> R.string.send_reason_bot_removed_group
            TargetType.CHANNEL -> R.string.send_reason_bot_removed_channel
            else -> R.string.send_reason_bot_removed_chat
        }
        InactiveReason.BOT_NOT_ADMIN -> R.string.send_reason_bot_not_admin
        InactiveReason.USER_LEFT -> R.string.send_reason_user_left
        InactiveReason.USER_NOT_ADMIN -> R.string.send_reason_user_not_admin
        InactiveReason.UNKNOWN, null -> R.string.send_reason_unknown
    },
)

@Composable
private fun CaptionField(state: SendUiState, onCaptionChange: (String) -> Unit) {
    val supporting = when {
        state.captionLeft < CAPTION_HINT_THRESHOLD ->
            pluralStringResource(R.plurals.send_caption_left, state.captionLeft, state.captionLeft)
        state.captionTouched && state.captionText.isEmpty() -> stringResource(R.string.send_caption_empty_hint)
        else -> null
    }
    QuokerTextArea(
        value = state.captionFieldValue,
        onValueChange = onCaptionChange,
        label = stringResource(R.string.send_caption_label),
        placeholder = stringResource(R.string.send_caption_placeholder),
        supportingText = supporting,
        enabled = !state.sending,
    )
}

@Composable
private fun SignToggle(state: SendUiState, onChange: (Boolean) -> Unit) {
    val title = state.signName?.let { stringResource(R.string.send_sign_as_user, it) }
        ?: stringResource(R.string.send_sign_as_user_generic)
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .defaultMinSize(minHeight = 48.dp)
            .toggleable(value = state.signAsUser, role = Role.Checkbox, onValueChange = onChange),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(title, style = QuokerTheme.typography.bodyLarge, color = QuokerTheme.colors.onSurface)
            Text(
                stringResource(R.string.send_sign_only_groups),
                style = QuokerTheme.typography.labelSmall.copy(fontWeight = FontWeight.W500),
                color = QuokerTheme.colors.onSurfaceVariant,
            )
        }
        QuokerCheckbox(checked = state.signAsUser, onCheckedChange = null)
    }
}

@Composable
private fun EmptyTargets(refreshing: Boolean, onRefresh: () -> Unit) {
    val c = QuokerTheme.colors
    val t = QuokerTheme.typography
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .background(c.surfaceVariant, RoundedCornerShape(QuokerTheme.dimens.cardRadius))
            .padding(QuokerTheme.dimens.cardPadding),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Text(stringResource(R.string.send_empty_title), style = t.titleSmall, color = c.onSurface)
        Text(stringResource(R.string.send_empty_group), style = t.bodyMedium, color = c.onSurfaceStrong)
        Text(stringResource(R.string.send_empty_channel), style = t.bodyMedium, color = c.onSurfaceStrong)
        Text(stringResource(R.string.send_empty_hint), style = t.bodySmall, color = c.onSurfaceVariant)
        Box(Modifier.padding(top = 4.dp)) {
            SecondaryButton(
                text = stringResource(R.string.send_refresh_list),
                onClick = onRefresh,
                size = ButtonSize.Compact,
                leadingIcon = QuokerIcons.Refresh,
                loading = refreshing,
            )
        }
    }
}

// Previews

private fun previewTarget(
    id: Long,
    title: String,
    type: TargetType,
    lastUsed: Boolean = false,
    reason: InactiveReason? = null,
) = Target(
    id = id,
    title = title,
    type = type,
    status = if (reason == null) TargetStatus.ACTIVE else TargetStatus.INACTIVE,
    inactiveReason = reason,
    hidden = false,
    lastUsedAt = if (lastUsed) Instant.parse("2026-11-02T12:40:00Z") else null,
)

private val previewTargets = orderTargets(
    listOf(
        previewTarget(42, "Друзья", TargetType.GROUP, lastUsed = true),
        previewTarget(43, "Мой канал", TargetType.CHANNEL),
        previewTarget(44, "Сохранённое в боте", TargetType.PRIVATE),
        previewTarget(45, "Старый канал", TargetType.CHANNEL, reason = InactiveReason.BOT_REMOVED),
    ),
)

private val previewReady = SendUiState(
    sessionId = "p",
    targets = previewTargets,
    fetched = true,
    selectedId = 42,
    defaultCaption = "https://www.tiktok.com/@author/video/7301234567890123456",
    signName = "@max",
)

@Composable
private fun PreviewSheet(state: SendUiState) {
    QuokerTheme {
        Box(Modifier.background(QuokerTheme.colors.background)) {
            SheetBody(spacing = 16.dp) {
                SendSheetContent(state, {}, {}, {}, {}, {}, {}, {})
            }
        }
    }
}

@Preview(name = "Ready, group selected")
@Composable
private fun SendReadyPreview() = PreviewSheet(previewReady)

@Preview(name = "Channel selected, caption cleared")
@Composable
private fun SendChannelPreview() =
    PreviewSheet(previewReady.copy(selectedId = 43, captionTouched = true, captionText = ""))

@Preview(name = "Sending")
@Composable
private fun SendSendingPreview() = PreviewSheet(previewReady.copy(sending = true))

@Preview(name = "Send error")
@Composable
private fun SendErrorPreview() = PreviewSheet(
    previewReady.copy(sendError = QuokerError("rate_limited", "Слишком часто", retryable = true)),
)

@Preview(name = "Caption near the limit")
@Composable
private fun SendCaptionLimitPreview() =
    PreviewSheet(previewReady.copy(captionTouched = true, captionText = "а".repeat(1000)))

@Preview(name = "Loading")
@Composable
private fun SendLoadingPreview() = PreviewSheet(SendUiState(sessionId = "p"))

@Preview(name = "Load failed")
@Composable
private fun SendLoadFailedPreview() = PreviewSheet(
    SendUiState(sessionId = "p", fetchError = QuokerError("network", "", retryable = true)),
)

@Preview(name = "Empty")
@Composable
private fun SendEmptyPreview() = PreviewSheet(SendUiState(sessionId = "p", fetched = true))

@Preview(name = "Empty with inactive, refreshing")
@Composable
private fun SendEmptyInactivePreview() = PreviewSheet(
    SendUiState(
        sessionId = "p",
        fetched = true,
        refreshing = true,
        targets = orderTargets(listOf(previewTarget(45, "Старый канал", TargetType.CHANNEL, reason = InactiveReason.BOT_NOT_ADMIN))),
    ),
)
