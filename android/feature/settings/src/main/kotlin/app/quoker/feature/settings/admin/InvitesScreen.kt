package app.quoker.feature.settings.admin

import android.content.ClipData
import android.content.ClipDescription
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.os.Build
import android.os.PersistableBundle
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role as SemanticsRole
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import app.quoker.core.data.model.Invite
import app.quoker.core.data.model.Role
import app.quoker.core.designsystem.component.ButtonSize
import app.quoker.core.designsystem.component.ButtonTone
import app.quoker.core.designsystem.component.ConfirmDialog
import app.quoker.core.designsystem.component.ListGroup
import app.quoker.core.designsystem.component.ListRow
import app.quoker.core.designsystem.component.PrimaryButton
import app.quoker.core.designsystem.component.PrimaryButtonVariant
import app.quoker.core.designsystem.component.QuokerTextButton
import app.quoker.core.designsystem.component.RowTextTone
import app.quoker.core.designsystem.component.SecondaryButton
import app.quoker.core.designsystem.component.SectionHeader
import app.quoker.core.designsystem.component.SegmentedControl
import app.quoker.core.designsystem.component.SkeletonBlock
import app.quoker.core.designsystem.component.TextButtonTone
import app.quoker.core.designsystem.icon.QuokerIcon
import app.quoker.core.designsystem.icon.QuokerIcons
import app.quoker.core.designsystem.theme.QuokerTheme
import app.quoker.feature.settings.R
import app.quoker.feature.settings.ui.LoadError
import app.quoker.feature.settings.ui.SettingsScaffold
import app.quoker.feature.settings.ui.dateText
import java.time.Instant
import kotlinx.coroutines.delay

@Composable
internal fun InvitesScreen(onBack: () -> Unit, viewModel: InvitesViewModel = hiltViewModel()) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    val context = LocalContext.current
    val clipLabel = stringResource(R.string.invites_clip_label)
    val shareTemplate = stringResource(R.string.invites_share_text)
    val shareTitle = stringResource(R.string.invites_share)
    InvitesContent(
        state = state,
        actions = InvitesActions(
            onBack = onBack,
            onRetry = viewModel::retry,
            onSelectRole = viewModel::selectRole,
            onSelectDays = viewModel::selectDays,
            onCreate = viewModel::create,
            onCopy = { code -> copyCode(context, clipLabel, code) },
            onShare = { code ->
                val send = Intent(Intent.ACTION_SEND).apply {
                    type = "text/plain"
                    putExtra(Intent.EXTRA_TEXT, shareTemplate.format(code))
                }
                context.startActivity(Intent.createChooser(send, shareTitle))
            },
            onRevoke = viewModel::askRevoke,
            onDismissMessage = viewModel::dismissMessage,
        ),
    )
    state.confirmRevoke?.let { invite ->
        ConfirmDialog(
            title = stringResource(R.string.invites_revoke_title, stringResource(R.string.invites_mask, invite.codePrefix)),
            text = stringResource(R.string.invites_revoke_text),
            confirmText = stringResource(R.string.invites_revoke),
            onConfirm = viewModel::revoke,
            onDismiss = viewModel::dismissRevoke,
            destructive = true,
        )
    }
}

private fun copyCode(context: Context, label: String, code: String) {
    val clipboard = context.getSystemService(ClipboardManager::class.java) ?: return
    val clip = ClipData.newPlainText(label, code)
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
        // Keeps the one-time code out of the clipboard preview.
        clip.description.extras = PersistableBundle().apply { putBoolean(ClipDescription.EXTRA_IS_SENSITIVE, true) }
    }
    clipboard.setPrimaryClip(clip)
}

internal data class InvitesActions(
    val onBack: () -> Unit = {},
    val onRetry: () -> Unit = {},
    val onSelectRole: (Role) -> Unit = {},
    val onSelectDays: (Int) -> Unit = {},
    val onCreate: () -> Unit = {},
    val onCopy: (String) -> Unit = {},
    val onShare: (String) -> Unit = {},
    val onRevoke: (Invite) -> Unit = {},
    val onDismissMessage: () -> Unit = {},
)

private val roles = listOf(Role.USER, Role.ADMIN)

@Composable
internal fun InvitesContent(state: InvitesUiState, actions: InvitesActions) {
    SettingsScaffold(
        title = stringResource(R.string.invites_title),
        onBack = actions.onBack,
        message = state.message,
        onDismissMessage = actions.onDismissMessage,
        spacing = 12.dp,
    ) {
        SegmentedControl(
            options = listOf(stringResource(R.string.role_user), stringResource(R.string.role_admin)),
            selectedIndex = roles.indexOf(state.role),
            onSelect = { actions.onSelectRole(roles[it]) },
        )
        DaysPicker(state.days, actions.onSelectDays)
        PrimaryButton(
            text = stringResource(R.string.invites_create),
            onClick = actions.onCreate,
            loading = state.creating,
        )
        state.created?.let { NewInviteCard(it, state.now, actions) }
        IssuedSection(state, actions)
    }
}

@Composable
private fun DaysPicker(days: Int, onSelect: (Int) -> Unit) {
    val c = QuokerTheme.colors
    val t = QuokerTheme.typography
    var expanded by remember { mutableStateOf(false) }
    Box {
        Row(
            Modifier
                .fillMaxWidth()
                .defaultMinSize(minHeight = 48.dp)
                .background(c.surface, RoundedCornerShape(14.dp))
                .clickable(role = SemanticsRole.DropdownList) { expanded = true }
                .padding(horizontal = 16.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(stringResource(R.string.invites_valid_for), style = t.bodyLarge, color = c.onSurface, modifier = Modifier.weight(1f))
            Text(pluralStringResource(R.plurals.invites_days, days, days), style = t.bodyLarge, color = c.onSurfaceVariant)
            QuokerIcon(QuokerIcons.ChevronDown, contentDescription = null, tint = c.onSurfaceVariant, size = 18.dp)
        }
        DropdownMenu(
            expanded = expanded,
            onDismissRequest = { expanded = false },
            containerColor = c.surfaceVariant,
            shape = RoundedCornerShape(14.dp),
        ) {
            InvitesUiState.DAY_OPTIONS.forEach { option ->
                DropdownMenuItem(
                    text = {
                        Text(
                            pluralStringResource(R.plurals.invites_days, option, option),
                            style = t.bodyLarge.copy(fontWeight = if (option == days) FontWeight.W700 else FontWeight.W600),
                            color = c.onSurface,
                        )
                    },
                    trailingIcon = if (option == days) {
                        { QuokerIcon(QuokerIcons.Check, contentDescription = null, tint = c.send, size = 18.dp) }
                    } else {
                        null
                    },
                    onClick = {
                        expanded = false
                        onSelect(option)
                    },
                )
            }
        }
    }
}

@Composable
private fun NewInviteCard(invite: NewInvite, now: Instant, actions: InvitesActions) {
    val c = QuokerTheme.colors
    val t = QuokerTheme.typography
    val role = stringResource(if (invite.role == Role.ADMIN) R.string.role_admin_lower else R.string.role_user_lower)
    var copied by rememberSaveable(invite.code) { mutableStateOf(false) }
    LaunchedEffect(copied) {
        if (copied) {
            delay(2_000)
            copied = false
        }
    }
    Column(
        Modifier
            .padding(top = 4.dp)
            .fillMaxWidth()
            .background(c.downloadContainer, RoundedCornerShape(16.dp))
            .padding(start = 16.dp, end = 16.dp, top = 18.dp, bottom = 16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text(
            invite.expiresAt?.let { stringResource(R.string.invites_new_code, role, dateText(it, now)) }
                ?: stringResource(R.string.invites_new_code_no_expiry, role),
            style = t.labelMedium.copy(fontWeight = FontWeight.W700),
            color = c.onDownloadContainer,
        )
        Text(
            invite.code,
            style = t.displaySmall.copy(fontSize = 32.sp, fontWeight = FontWeight.W800, letterSpacing = 4.sp),
            color = c.onSurface,
        )
        Text(stringResource(R.string.invites_shown_once), style = t.bodySmall, color = c.onSurfaceStrong)
        Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            PrimaryButton(
                text = stringResource(if (copied) R.string.invites_copied else R.string.invites_copy),
                onClick = {
                    actions.onCopy(invite.code)
                    copied = true
                },
                modifier = Modifier.weight(1f),
                variant = PrimaryButtonVariant.Download,
                size = ButtonSize.Small,
                fullWidth = true,
            )
            SecondaryButton(
                text = stringResource(R.string.invites_share),
                onClick = { actions.onShare(invite.code) },
                modifier = Modifier.weight(1f),
                size = ButtonSize.Small,
                tone = ButtonTone.OnDownloadContainer,
                fullWidth = true,
            )
        }
    }
}

@Composable
private fun IssuedSection(state: InvitesUiState, actions: InvitesActions) {
    val invites = state.invites
    Column(Modifier.padding(top = 8.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        when {
            invites == null && state.error != null -> LoadError(state.error, actions.onRetry)
            invites == null -> {
                SectionHeader(stringResource(R.string.invites_section_issued))
                SkeletonBlock(Modifier.fillMaxWidth().height(180.dp), RoundedCornerShape(16.dp))
            }
            invites.isEmpty() -> {
                SectionHeader(stringResource(R.string.invites_section_issued))
                Text(
                    stringResource(R.string.invites_empty),
                    style = QuokerTheme.typography.bodyMedium,
                    color = QuokerTheme.colors.onSurfaceVariant,
                )
            }
            else -> ListGroup(header = stringResource(R.string.invites_section_issued)) {
                invites.forEachIndexed { index, invite ->
                    InviteRow(invite, state, divider = index < invites.lastIndex, onRevoke = { actions.onRevoke(invite) })
                }
            }
        }
    }
}

@Composable
private fun InviteRow(invite: Invite, state: InvitesUiState, divider: Boolean, onRevoke: () -> Unit) {
    val role = stringResource(if (invite.role == Role.ADMIN) R.string.role_admin else R.string.role_user)
    val expired = !invite.isUsed && invite.expiresAt?.isAfter(state.now) == false
    val subtitle = when {
        invite.isUsed -> state.userNames[invite.usedBy]?.let { stringResource(R.string.invites_used_by, it) }
            ?: stringResource(R.string.invites_used)
        invite.expiresAt == null -> role
        expired -> stringResource(R.string.invites_expired, role, dateText(invite.expiresAt!!, state.now))
        else -> stringResource(R.string.invites_until, role, dateText(invite.expiresAt!!, state.now))
    }
    val inactive = invite.isUsed || expired
    ListRow(
        title = stringResource(R.string.invites_mask, invite.codePrefix),
        subtitle = subtitle,
        titleTone = if (inactive) RowTextTone.Muted else RowTextTone.Default,
        divider = divider,
        trailing = if (invite.isUsed) {
            null
        } else {
            {
                QuokerTextButton(
                    text = stringResource(R.string.invites_revoke),
                    onClick = onRevoke,
                    tone = TextButtonTone.Danger,
                    enabled = state.revokingId != invite.id,
                    contentPadding = androidx.compose.foundation.layout.PaddingValues(horizontal = 12.dp, vertical = 10.dp),
                )
            }
        },
    )
}

// Previews

private val previewNow = Instant.parse("2026-11-02T12:00:00Z")

internal val previewInvites = listOf(
    Invite(1, Role.USER, "K7Q2", previewNow, Instant.parse("2026-11-09T12:00:00Z"), null, null),
    Invite(2, Role.USER, "B8RW", previewNow, Instant.parse("2026-11-06T12:00:00Z"), null, null),
    Invite(3, Role.USER, "P3ZN", previewNow, Instant.parse("2026-11-06T12:00:00Z"), 7, previewNow),
)

@Preview(widthDp = 390, heightDp = 844)
@Composable
private fun InvitesCreatedPreview() {
    QuokerTheme {
        InvitesContent(
            InvitesUiState(
                created = NewInvite("K7Q2-M4XP", Role.USER, Instant.parse("2026-11-09T12:00:00Z")),
                invites = previewInvites,
                userNames = mapOf(7L to "@friend"),
                now = previewNow,
            ),
            InvitesActions(),
        )
    }
}

@Preview(widthDp = 390, heightDp = 844)
@Composable
private fun InvitesLoadingPreview() {
    QuokerTheme { InvitesContent(InvitesUiState(role = Role.ADMIN, days = 30, creating = true, now = previewNow), InvitesActions()) }
}

@Preview(widthDp = 390, heightDp = 844)
@Composable
private fun InvitesEmptyPreview() {
    QuokerTheme { InvitesContent(InvitesUiState(invites = emptyList(), now = previewNow), InvitesActions()) }
}
