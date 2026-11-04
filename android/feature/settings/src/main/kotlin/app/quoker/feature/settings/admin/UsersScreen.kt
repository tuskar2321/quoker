package app.quoker.feature.settings.admin

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import app.quoker.core.data.model.AdminUser
import app.quoker.core.data.model.Role
import app.quoker.core.data.model.UserStatus
import app.quoker.core.designsystem.component.AvatarTone
import app.quoker.core.designsystem.component.BadgeTone
import app.quoker.core.designsystem.component.ButtonSize
import app.quoker.core.designsystem.component.ButtonTone
import app.quoker.core.designsystem.component.ConfirmDialog
import app.quoker.core.designsystem.component.InitialAvatar
import app.quoker.core.designsystem.component.PrimaryButton
import app.quoker.core.designsystem.component.QuokerBadge
import app.quoker.core.designsystem.component.SecondaryButton
import app.quoker.core.designsystem.theme.QuokerTheme
import app.quoker.feature.settings.R
import app.quoker.feature.settings.ui.CardSkeletons
import app.quoker.feature.settings.ui.CardSubtitle
import app.quoker.feature.settings.ui.EntityCard
import app.quoker.feature.settings.ui.LoadError
import app.quoker.feature.settings.ui.Note
import app.quoker.feature.settings.ui.SettingsScaffold
import app.quoker.feature.settings.ui.capitalizeFirst
import app.quoker.feature.settings.ui.displayName
import app.quoker.feature.settings.ui.hasTelegram
import app.quoker.feature.settings.ui.lastSeenText
import app.quoker.feature.settings.ui.resources
import java.time.Instant

@Composable
internal fun UsersScreen(onBack: () -> Unit, viewModel: UsersViewModel = hiltViewModel()) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    UsersContent(
        state = state,
        onBack = onBack,
        onRetry = viewModel::retry,
        onAction = viewModel::ask,
        onDismissMessage = viewModel::dismissMessage,
    )
    state.confirm?.let { action ->
        val name = action.user.displayName(resources())
        ConfirmDialog(
            title = stringResource(if (action.block) R.string.users_block_title else R.string.users_unblock_title, name),
            text = stringResource(if (action.block) R.string.users_block_text else R.string.users_unblock_text),
            confirmText = stringResource(if (action.block) R.string.users_block else R.string.users_unblock),
            onConfirm = viewModel::confirm,
            onDismiss = viewModel::dismissConfirm,
            destructive = action.block,
        )
    }
}

@Composable
internal fun UsersContent(
    state: UsersUiState,
    onBack: () -> Unit = {},
    onRetry: () -> Unit = {},
    onAction: (AdminUser) -> Unit = {},
    onDismissMessage: () -> Unit = {},
) {
    SettingsScaffold(
        title = stringResource(R.string.users_title),
        onBack = onBack,
        message = state.message,
        onDismissMessage = onDismissMessage,
    ) {
        val users = state.users
        when {
            users == null && state.error != null -> LoadError(state.error, onRetry)
            users == null -> CardSkeletons(count = 4)
            users.isEmpty() -> CardSubtitle(stringResource(R.string.users_empty))
            else -> {
                Text(
                    stringResource(
                        R.string.users_counters,
                        pluralStringResource(R.plurals.users_count_people, state.total, state.total),
                        pluralStringResource(R.plurals.users_count_active, state.active, state.active),
                        pluralStringResource(R.plurals.admin_users_blocked_short, state.blocked, state.blocked),
                    ),
                    style = QuokerTheme.typography.bodySmall,
                    color = QuokerTheme.colors.onSurfaceVariant,
                )
                users.forEach { user ->
                    UserCard(user, isMe = user.id == state.myId, busy = state.busyId == user.id, now = state.now, onAction = { onAction(user) })
                }
                Note(stringResource(R.string.users_note))
            }
        }
    }
}

@Composable
private fun UserCard(user: AdminUser, isMe: Boolean, busy: Boolean, now: Instant, onAction: () -> Unit) {
    val res = resources()
    val blocked = user.status == UserStatus.BLOCKED
    val name = user.displayName(res)
    EntityCard(
        title = name,
        titleMuted = blocked,
        leading = {
            InitialAvatar(
                initial = user.telegramUsername ?: user.telegramFirstName ?: name.removePrefix("@"),
                tone = when {
                    blocked -> AvatarTone.Error
                    isMe -> AvatarTone.Download
                    else -> AvatarTone.Neutral
                },
            )
        },
        subtitle = {
            if (blocked) {
                Column(Modifier, verticalArrangement = Arrangement.spacedBy(2.dp)) {
                    QuokerBadge(stringResource(R.string.users_blocked), BadgeTone.Error)
                }
            } else {
                CardSubtitle(userSubtitle(user, isMe, now))
            }
        },
        trailing = when {
            isMe -> null
            blocked -> {
                {
                    PrimaryButton(
                        text = stringResource(R.string.users_unblock),
                        onClick = onAction,
                        size = ButtonSize.Compact,
                        loading = busy,
                    )
                }
            }
            else -> {
                {
                    SecondaryButton(
                        text = stringResource(R.string.users_block),
                        onClick = onAction,
                        size = ButtonSize.Compact,
                        tone = ButtonTone.Danger,
                        loading = busy,
                    )
                }
            }
        },
    )
}

/** «Админ · это вы», «Сегодня, 12:40 · 3 цели», «Telegram не привязан». */
@Composable
private fun userSubtitle(user: AdminUser, isMe: Boolean, now: Instant): String {
    val res = resources()
    val parts = buildList {
        if (user.role == Role.ADMIN) add(stringResource(R.string.users_admin))
        if (isMe) {
            add(stringResource(R.string.users_you))
        } else if (!user.hasTelegram) {
            add(stringResource(R.string.users_telegram_not_linked))
        } else {
            add(user.lastSeenAt?.let { lastSeenText(res, it, now) } ?: stringResource(R.string.users_never_seen))
            add(pluralStringResource(R.plurals.users_targets, user.targetsCount, user.targetsCount))
        }
    }
    return parts.joinToString(" · ").capitalizeFirst()
}

// Previews

private val previewNow = Instant.parse("2026-11-05T12:00:00Z")

internal val previewUsers = listOf(
    AdminUser(1, Role.ADMIN, UserStatus.ACTIVE, "username", "Max", 3, previewNow, previewNow),
    AdminUser(2, Role.USER, UserStatus.ACTIVE, "friend", "Аня", 3, previewNow, Instant.parse("2026-11-05T09:40:00Z")),
    AdminUser(3, Role.USER, UserStatus.ACTIVE, "friend2", "Костя", 1, previewNow, Instant.parse("2026-11-04T18:00:00Z")),
    AdminUser(4, Role.USER, UserStatus.ACTIVE, null, null, 0, previewNow, null),
    AdminUser(5, Role.USER, UserStatus.BLOCKED, "someone", "S", 0, previewNow, null),
)

@Preview(widthDp = 390, heightDp = 844)
@Composable
private fun UsersPreview() {
    QuokerTheme { UsersContent(UsersUiState(users = previewUsers, myId = 1, busyId = 3, now = previewNow)) }
}

@Preview(widthDp = 390, heightDp = 844)
@Composable
private fun UsersLoadingPreview() {
    QuokerTheme { UsersContent(UsersUiState(now = previewNow)) }
}

@Preview(widthDp = 390, heightDp = 844)
@Composable
private fun UsersErrorPreview() {
    QuokerTheme { UsersContent(UsersUiState(error = app.quoker.core.common.QuokerError("network", "", true), now = previewNow)) }
}
