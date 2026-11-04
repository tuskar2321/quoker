package app.quoker.feature.settings

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.role
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.compose.foundation.selection.toggleable
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import app.quoker.core.common.QuokerError
import app.quoker.core.data.model.InactiveReason
import app.quoker.core.data.model.Me
import app.quoker.core.data.model.Role as UserRole
import app.quoker.core.data.model.Target
import app.quoker.core.data.model.TargetStatus
import app.quoker.core.data.model.TargetType
import app.quoker.core.data.model.TelegramAccount
import app.quoker.core.designsystem.component.AvatarTone
import app.quoker.core.designsystem.component.ButtonSize
import app.quoker.core.designsystem.component.ButtonTone
import app.quoker.core.designsystem.component.ConfirmDialog
import app.quoker.core.designsystem.component.InitialAvatar
import app.quoker.core.designsystem.component.ListGroup
import app.quoker.core.designsystem.component.ListRow
import app.quoker.core.designsystem.component.ListRowChevron
import app.quoker.core.designsystem.component.PrimaryButton
import app.quoker.core.designsystem.component.PrimaryButtonVariant
import app.quoker.core.designsystem.component.QuokerCheckbox
import app.quoker.core.designsystem.component.QuokerIconButton
import app.quoker.core.designsystem.component.QuokerTextButton
import app.quoker.core.designsystem.component.RowTextTone
import app.quoker.core.designsystem.component.SecondaryButton
import app.quoker.core.designsystem.component.SectionHeader
import app.quoker.core.designsystem.component.SkeletonBlock
import app.quoker.core.designsystem.icon.QuokerIcon
import app.quoker.core.designsystem.icon.QuokerIcons
import app.quoker.core.designsystem.theme.QuokerTheme
import app.quoker.core.designsystem.text.userMessage
import app.quoker.feature.settings.ui.SettingsScaffold
import app.quoker.feature.settings.ui.UiMessage

@Composable
internal fun SettingsScreen(
    onBack: () -> Unit,
    onLinkTelegram: () -> Unit,
    onOpenAdmin: () -> Unit,
    onCheckUpdates: () -> Unit,
    onSignedOut: () -> Unit,
    viewModel: SettingsViewModel = hiltViewModel(),
) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { viewModel.onResume() }
    LaunchedEffect(viewModel) {
        viewModel.events.collect { event ->
            when (event) {
                SettingsEvent.SignedOut -> onSignedOut()
            }
        }
    }
    SettingsContent(
        state = state,
        actions = SettingsActions(
            onBack = onBack,
            onLinkTelegram = onLinkTelegram,
            onUnlink = { viewModel.showDialog(SettingsDialog.Unlink) },
            onRetry = viewModel::retry,
            onRefreshTargets = viewModel::refreshTargets,
            onToggleTarget = viewModel::setVisible,
            onHowToAdd = { viewModel.showDialog(SettingsDialog.HowToAdd) },
            onOpenAdmin = onOpenAdmin,
            onCheckUpdates = onCheckUpdates,
            onLogout = { viewModel.showDialog(SettingsDialog.Logout) },
            onDismissMessage = viewModel::dismissMessage,
        ),
    )
    when (state.dialog) {
        SettingsDialog.Unlink -> ConfirmDialog(
            title = stringResource(R.string.settings_unlink_title),
            text = stringResource(R.string.settings_unlink_text),
            confirmText = stringResource(R.string.settings_telegram_unlink),
            onConfirm = viewModel::unlinkTelegram,
            onDismiss = viewModel::dismissDialog,
            destructive = true,
        )
        SettingsDialog.Logout -> ConfirmDialog(
            title = stringResource(R.string.settings_logout_title),
            text = stringResource(R.string.settings_logout_text),
            confirmText = stringResource(R.string.settings_logout),
            onConfirm = viewModel::logout,
            onDismiss = viewModel::dismissDialog,
            destructive = true,
        )
        SettingsDialog.HowToAdd -> HowToAddSheet(onDismiss = viewModel::dismissDialog)
        null -> Unit
    }
}

internal data class SettingsActions(
    val onBack: () -> Unit = {},
    val onLinkTelegram: () -> Unit = {},
    val onUnlink: () -> Unit = {},
    val onRetry: () -> Unit = {},
    val onRefreshTargets: () -> Unit = {},
    val onToggleTarget: (Long, Boolean) -> Unit = { _, _ -> },
    val onHowToAdd: () -> Unit = {},
    val onOpenAdmin: () -> Unit = {},
    val onCheckUpdates: () -> Unit = {},
    val onLogout: () -> Unit = {},
    val onDismissMessage: () -> Unit = {},
)

@Composable
internal fun SettingsContent(state: SettingsUiState, actions: SettingsActions) {
    SettingsScaffold(
        title = stringResource(R.string.settings_title),
        onBack = actions.onBack,
        message = state.message,
        onDismissMessage = actions.onDismissMessage,
        spacing = 0.dp,
    ) {
        Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
            SectionHeader(stringResource(R.string.settings_section_telegram), Modifier.padding(top = 8.dp))
            TelegramCard(state, actions)
        }
        TargetsSection(state, actions)
        AppSection(state, actions)
    }
}

@Composable
private fun TelegramCard(state: SettingsUiState, actions: SettingsActions) {
    val c = QuokerTheme.colors
    val t = QuokerTheme.typography
    val telegram = state.telegram
    val cardModifier = Modifier
        .fillMaxWidth()
        .background(c.surface, RoundedCornerShape(QuokerTheme.dimens.cardRadius))
        .padding(horizontal = 16.dp, vertical = 14.dp)
    when {
        telegram == null && state.meError != null -> Column(cardModifier, verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(state.meError.userMessage(), style = t.bodyMedium, color = c.onSurfaceStrong)
            QuokerTextButton(stringResource(app.quoker.core.designsystem.R.string.action_retry), actions.onRetry)
        }
        telegram == null -> SkeletonBlock(Modifier.fillMaxWidth().height(72.dp), RoundedCornerShape(16.dp))
        telegram.linked -> Row(
            cardModifier,
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            val name = telegram.username?.let { "@$it" } ?: telegram.firstName.orEmpty()
            InitialAvatar(
                initial = telegram.username ?: telegram.firstName ?: "",
                tone = AvatarTone.Send,
                size = QuokerTheme.dimens.avatarLarge,
            )
            Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Text(
                    name,
                    style = t.bodyLarge.copy(fontWeight = FontWeight.W700),
                    color = c.onSurface,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                Text(stringResource(R.string.settings_telegram_linked), style = t.bodySmall, color = c.onSurfaceVariant)
            }
            SecondaryButton(
                text = stringResource(R.string.settings_telegram_unlink),
                onClick = actions.onUnlink,
                size = ButtonSize.Compact,
                tone = ButtonTone.Danger,
                loading = state.unlinking,
            )
        }
        else -> Column(cardModifier, verticalArrangement = Arrangement.spacedBy(14.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(14.dp)) {
                InitialAvatar("", tone = AvatarTone.Neutral, size = QuokerTheme.dimens.avatarLarge, icon = QuokerIcons.Send)
                Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                    Text(
                        stringResource(R.string.settings_telegram_not_linked),
                        style = t.bodyLarge.copy(fontWeight = FontWeight.W700),
                        color = c.onSurface,
                    )
                    Text(stringResource(R.string.settings_telegram_not_linked_hint), style = t.bodySmall, color = c.onSurfaceVariant)
                }
            }
            PrimaryButton(
                text = stringResource(R.string.settings_telegram_link),
                onClick = actions.onLinkTelegram,
                variant = PrimaryButtonVariant.Send,
                size = ButtonSize.Small,
                leadingIcon = QuokerIcons.Send,
                fullWidth = true,
            )
        }
    }
}

@Composable
private fun TargetsSection(state: SettingsUiState, actions: SettingsActions) {
    val c = QuokerTheme.colors
    Column(Modifier.padding(top = 24.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
        SectionHeader(stringResource(R.string.settings_section_targets)) {
            if (state.refreshingTargets) {
                androidx.compose.foundation.layout.Box(Modifier.size(44.dp), contentAlignment = Alignment.Center) {
                    CircularProgressIndicator(Modifier.size(18.dp), color = c.onSurfaceVariant, strokeWidth = 2.dp)
                }
            } else {
                QuokerIconButton(
                    icon = QuokerIcons.Refresh,
                    contentDescription = stringResource(R.string.settings_targets_refresh),
                    onClick = actions.onRefreshTargets,
                    tint = c.onSurfaceVariant,
                    size = 44.dp,
                    iconSize = 20.dp,
                )
            }
        }
        when {
            state.targets.isNotEmpty() -> ListGroup {
                state.targets.forEachIndexed { index, row ->
                    TargetListRow(row, divider = index < state.targets.lastIndex, onToggle = actions.onToggleTarget)
                }
            }
            !state.targetsLoaded -> SkeletonBlock(Modifier.fillMaxWidth().height(120.dp), RoundedCornerShape(16.dp))
            else -> Column(
                Modifier
                    .fillMaxWidth()
                    .background(c.surface, RoundedCornerShape(16.dp))
                    .padding(horizontal = 16.dp, vertical = 14.dp),
                verticalArrangement = Arrangement.spacedBy(2.dp),
            ) {
                Text(
                    stringResource(R.string.settings_targets_empty_title),
                    style = QuokerTheme.typography.bodyLarge,
                    color = c.onSurface,
                )
                Text(
                    stringResource(R.string.settings_targets_empty_text),
                    style = QuokerTheme.typography.bodySmall,
                    color = c.onSurfaceVariant,
                )
            }
        }
        QuokerTextButton(stringResource(R.string.settings_how_to_add), actions.onHowToAdd)
    }
}

@Composable
private fun TargetListRow(row: TargetRow, divider: Boolean, onToggle: (Long, Boolean) -> Unit) {
    val target = row.target
    val active = target.isActive
    ListRow(
        title = target.title,
        subtitle = if (active) targetTypeText(target.type) else inactiveReasonText(target),
        titleTone = if (active) RowTextTone.Default else RowTextTone.Muted,
        subtitleTone = if (active) RowTextTone.Muted else RowTextTone.Error,
        divider = divider,
        modifier = Modifier.then(
            if (active) {
                Modifier.toggleable(
                    value = row.visible,
                    enabled = !row.saving,
                    role = Role.Checkbox,
                    onValueChange = { onToggle(target.id, it) },
                )
            } else {
                Modifier.semantics { role = Role.Checkbox }
            },
        ),
        trailing = {
            QuokerCheckbox(
                checked = active && row.visible,
                onCheckedChange = null,
                enabled = active && !row.saving,
            )
        },
    )
}

@Composable
internal fun targetTypeText(type: TargetType): String = stringResource(
    when (type) {
        TargetType.GROUP -> R.string.target_type_group
        TargetType.CHANNEL -> R.string.target_type_channel
        TargetType.PRIVATE -> R.string.target_type_private
        TargetType.UNKNOWN -> R.string.target_type_unknown
    },
)

@Composable
internal fun inactiveReasonText(target: Target): String = stringResource(
    when (target.inactiveReason) {
        InactiveReason.BOT_REMOVED ->
            if (target.type == TargetType.CHANNEL) R.string.target_reason_bot_removed_channel else R.string.target_reason_bot_removed_group
        InactiveReason.BOT_NOT_ADMIN -> R.string.target_reason_bot_not_admin
        InactiveReason.USER_LEFT ->
            if (target.type == TargetType.PRIVATE) R.string.target_reason_user_left_private else R.string.target_reason_user_left
        InactiveReason.USER_NOT_ADMIN -> R.string.target_reason_user_not_admin
        InactiveReason.UNKNOWN, null -> R.string.target_reason_unknown
    },
)

@Composable
private fun AppSection(state: SettingsUiState, actions: SettingsActions) {
    val c = QuokerTheme.colors
    Column(Modifier.padding(top = 12.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        ListGroup(header = stringResource(R.string.settings_section_app)) {
            if (state.isAdmin) {
                ListRow(
                    title = stringResource(R.string.settings_admin),
                    leading = { QuokerIcon(QuokerIcons.Shield, null, tint = c.download, size = 20.dp) },
                    trailing = { ListRowChevron() },
                    onClick = actions.onOpenAdmin,
                    divider = true,
                    minHeight = QuokerTheme.dimens.listRowMinHeight,
                )
            }
            ListRow(
                title = stringResource(R.string.settings_version),
                subtitle = state.versionName,
                minHeight = QuokerTheme.dimens.listRowMinHeight,
                trailing = {
                    SecondaryButton(
                        text = stringResource(R.string.settings_check_updates),
                        onClick = actions.onCheckUpdates,
                        size = ButtonSize.Compact,
                    )
                },
            )
        }
        Spacer(Modifier.height(4.dp))
        SecondaryButton(
            text = stringResource(R.string.settings_logout),
            onClick = actions.onLogout,
            size = ButtonSize.Medium,
            tone = ButtonTone.Danger,
            loading = state.signingOut,
        )
    }
}

// Previews

private fun previewTarget(id: Long, title: String, type: TargetType, reason: InactiveReason? = null, hidden: Boolean = false) =
    Target(
        id = id,
        title = title,
        type = type,
        status = if (reason == null) TargetStatus.ACTIVE else TargetStatus.INACTIVE,
        inactiveReason = reason,
        hidden = hidden,
        lastUsedAt = null,
    )

internal val previewTargets = listOf(
    previewTarget(1, "Друзья", TargetType.GROUP),
    previewTarget(2, "Мой канал", TargetType.CHANNEL),
    previewTarget(3, "Сохранённое в боте", TargetType.PRIVATE, hidden = true),
    previewTarget(4, "Старый канал", TargetType.CHANNEL, InactiveReason.BOT_REMOVED),
).map { TargetRow(it, visible = !it.hidden, saving = false) }

internal fun previewMe(admin: Boolean, linked: Boolean = true) = Me(
    id = 1,
    role = if (admin) UserRole.ADMIN else UserRole.USER,
    telegram = if (linked) TelegramAccount(true, "username", "Max") else TelegramAccount(false, null, null),
)

@Preview(widthDp = 390, heightDp = 844)
@Composable
private fun SettingsAdminPreview() {
    QuokerTheme {
        SettingsContent(
            SettingsUiState(me = previewMe(admin = true), targets = previewTargets, targetsLoaded = true, versionName = "1.0.0"),
            SettingsActions(),
        )
    }
}

@Preview(widthDp = 390, heightDp = 844)
@Composable
private fun SettingsUserNotLinkedPreview() {
    QuokerTheme {
        SettingsContent(
            SettingsUiState(me = previewMe(admin = false, linked = false), targetsLoaded = true, versionName = "1.0.0"),
            SettingsActions(),
        )
    }
}

@Preview(widthDp = 390, heightDp = 844)
@Composable
private fun SettingsLoadingPreview() {
    QuokerTheme {
        SettingsContent(SettingsUiState(versionName = "1.0.0", refreshingTargets = true), SettingsActions())
    }
}

@Preview(widthDp = 390, heightDp = 844)
@Composable
private fun SettingsErrorPreview() {
    QuokerTheme {
        SettingsContent(
            SettingsUiState(
                meError = QuokerError("network", "", true),
                message = UiMessage.Failure(QuokerError("rate_limited", "", true)),
                versionName = "1.0.0",
            ),
            SettingsActions(),
        )
    }
}
