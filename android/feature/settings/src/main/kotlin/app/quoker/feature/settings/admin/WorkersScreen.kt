package app.quoker.feature.settings.admin

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import app.quoker.core.common.QuokerError
import app.quoker.core.data.model.Worker
import app.quoker.core.data.model.WorkerStatus
import app.quoker.core.designsystem.component.BadgeTone
import app.quoker.core.designsystem.component.ButtonSize
import app.quoker.core.designsystem.component.ButtonTone
import app.quoker.core.designsystem.component.ConfirmDialog
import app.quoker.core.designsystem.component.PrimaryButton
import app.quoker.core.designsystem.component.QuokerBadge
import app.quoker.core.designsystem.component.QuokerTextButton
import app.quoker.core.designsystem.component.QuokerTextField
import app.quoker.core.designsystem.component.SecondaryButton
import app.quoker.core.designsystem.component.StatusDot
import app.quoker.core.designsystem.component.TextButtonTone
import app.quoker.core.designsystem.text.userMessage
import app.quoker.core.designsystem.theme.QuokerTheme
import app.quoker.feature.settings.R
import app.quoker.feature.settings.ui.CardSkeletons
import app.quoker.feature.settings.ui.CardSubtitle
import app.quoker.feature.settings.ui.EntityCard
import app.quoker.feature.settings.ui.LoadError
import app.quoker.feature.settings.ui.Note
import app.quoker.feature.settings.ui.SettingsScaffold
import app.quoker.feature.settings.ui.dateText
import app.quoker.feature.settings.ui.lastSeenText
import app.quoker.feature.settings.ui.resources
import java.time.Instant
import app.quoker.core.designsystem.R as DesignR

@Composable
internal fun WorkersScreen(onBack: () -> Unit, viewModel: WorkersViewModel = hiltViewModel()) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    WorkersContent(
        state = state,
        onBack = onBack,
        onRetry = viewModel::retry,
        onAllow = viewModel::openAllow,
        onRevoke = viewModel::askRevoke,
        onDismissMessage = viewModel::dismissMessage,
    )
    state.confirmRevoke?.let { worker ->
        ConfirmDialog(
            title = stringResource(R.string.workers_revoke_title, worker.id),
            text = stringResource(R.string.workers_revoke_text),
            confirmText = stringResource(R.string.workers_revoke),
            onConfirm = viewModel::revoke,
            onDismiss = viewModel::dismissRevoke,
            destructive = true,
        )
    }
    state.allowForm?.let { form ->
        AllowWorkerDialog(
            form = form,
            onIdChange = viewModel::onAllowIdChange,
            onFingerprintChange = viewModel::onAllowFingerprintChange,
            onConfirm = viewModel::allow,
            onDismiss = viewModel::dismissAllow,
        )
    }
}

@Composable
internal fun WorkersContent(
    state: WorkersUiState,
    onBack: () -> Unit = {},
    onRetry: () -> Unit = {},
    onAllow: () -> Unit = {},
    onRevoke: (Worker) -> Unit = {},
    onDismissMessage: () -> Unit = {},
) {
    SettingsScaffold(
        title = stringResource(R.string.workers_title),
        onBack = onBack,
        message = state.message,
        onDismissMessage = onDismissMessage,
    ) {
        val workers = state.workers
        when {
            workers == null && state.error != null -> LoadError(state.error, onRetry)
            workers == null -> CardSkeletons()
            else -> {
                if (workers.isEmpty()) CardSubtitle(stringResource(R.string.workers_empty))
                workers.forEach { worker ->
                    WorkerCard(worker, revoking = state.revokingId == worker.id, now = state.now, onRevoke = { onRevoke(worker) })
                }
                PrimaryButton(text = stringResource(R.string.workers_allow), onClick = onAllow)
                Note(stringResource(R.string.workers_note))
            }
        }
    }
}

@Composable
private fun WorkerCard(worker: Worker, revoking: Boolean, now: Instant, onRevoke: () -> Unit) {
    val res = resources()
    val revoked = worker.status == WorkerStatus.REVOKED
    val unknown = stringResource(R.string.workers_version_unknown)
    EntityCard(
        title = worker.id,
        titleMuted = revoked || !worker.connected,
        leading = {
            Box(Modifier.size(QuokerTheme.dimens.avatar), contentAlignment = Alignment.Center) {
                StatusDot(active = worker.connected && !revoked)
            }
        },
        subtitle = {
            if (revoked) {
                QuokerBadge(stringResource(R.string.workers_revoked), BadgeTone.Error)
            } else {
                val presence = buildList {
                    if (worker.connected) {
                        add(stringResource(R.string.workers_online))
                    } else {
                        add(stringResource(R.string.workers_offline))
                        worker.lastSeenAt?.let { add(stringResource(R.string.workers_last_seen, lastSeenText(res, it, now))) }
                    }
                    if (worker.isPoller && worker.connected) add(stringResource(R.string.workers_poller))
                }
                CardSubtitle(presence.joinToString(" · "))
            }
            CardSubtitle(stringResource(R.string.workers_versions, worker.version ?: unknown, worker.ytdlpVersion ?: unknown))
            if (!revoked) {
                worker.certNotAfter?.let { notAfter ->
                    val date = dateText(notAfter, now)
                    when (certExpiry(notAfter, now)) {
                        CertExpiry.Valid -> CardSubtitle(stringResource(R.string.workers_cert_until, date))
                        CertExpiry.ExpiresSoon -> CardSubtitle(stringResource(R.string.workers_cert_until, date), error = true)
                        CertExpiry.Expired -> CardSubtitle(stringResource(R.string.workers_cert_expired, date), error = true)
                    }
                }
            }
        },
        trailing = if (revoked) {
            null
        } else {
            {
                SecondaryButton(
                    text = stringResource(R.string.workers_revoke),
                    onClick = onRevoke,
                    size = ButtonSize.Compact,
                    tone = ButtonTone.Danger,
                    loading = revoking,
                )
            }
        },
    )
}

@Composable
internal fun AllowWorkerDialog(
    form: AllowWorkerForm,
    onIdChange: (String) -> Unit,
    onFingerprintChange: (String) -> Unit,
    onConfirm: () -> Unit,
    onDismiss: () -> Unit,
) {
    val c = QuokerTheme.colors
    AlertDialog(
        onDismissRequest = onDismiss,
        containerColor = c.surface,
        titleContentColor = c.onSurface,
        textContentColor = c.onSurfaceStrong,
        tonalElevation = 0.dp,
        shape = RoundedCornerShape(28.dp),
        title = { Text(stringResource(R.string.workers_allow_title), style = QuokerTheme.typography.titleMedium) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(16.dp)) {
                QuokerTextField(
                    value = form.id,
                    onValueChange = onIdChange,
                    label = stringResource(R.string.workers_allow_id),
                    placeholder = stringResource(R.string.workers_allow_id_placeholder),
                    enabled = !form.submitting,
                    keyboardOptions = KeyboardOptions(autoCorrectEnabled = false, imeAction = ImeAction.Next),
                )
                QuokerTextField(
                    value = form.fingerprint,
                    onValueChange = onFingerprintChange,
                    label = stringResource(R.string.workers_allow_fingerprint),
                    supportingText = when {
                        form.error != null -> form.error.userMessage()
                        form.showFingerprintError -> stringResource(R.string.workers_allow_fingerprint_invalid)
                        else -> stringResource(R.string.workers_allow_fingerprint_hint)
                    },
                    isError = form.error != null || form.showFingerprintError,
                    enabled = !form.submitting,
                    keyboardOptions = KeyboardOptions(
                        capitalization = KeyboardCapitalization.None,
                        autoCorrectEnabled = false,
                        imeAction = ImeAction.Done,
                    ),
                )
            }
        },
        confirmButton = {
            if (form.submitting) {
                Box(Modifier.size(44.dp), contentAlignment = Alignment.Center) {
                    androidx.compose.material3.CircularProgressIndicator(Modifier.size(18.dp), color = c.send, strokeWidth = 2.dp)
                }
            } else {
                QuokerTextButton(
                    stringResource(R.string.workers_allow_confirm),
                    onConfirm,
                    enabled = form.canSubmit,
                    contentPadding = PaddingValues(horizontal = 12.dp, vertical = 12.dp),
                )
            }
        },
        dismissButton = {
            QuokerTextButton(
                stringResource(DesignR.string.action_cancel),
                onDismiss,
                tone = TextButtonTone.Plain,
                enabled = !form.submitting,
                contentPadding = PaddingValues(horizontal = 12.dp, vertical = 12.dp),
            )
        },
    )
}

// Previews

private val previewNow = Instant.parse("2026-11-05T12:00:00Z")

@Preview(widthDp = 390, heightDp = 844)
@Composable
private fun WorkersPreview() {
    QuokerTheme {
        WorkersContent(
            WorkersUiState(
                workers = listOf(
                    previewWorker("worker-1", poller = true),
                    previewWorker("worker-2", connected = false).copy(certNotAfter = previewNow.plusSeconds(86_400L * 12)),
                    previewWorker("worker-old", connected = false, revoked = true),
                ),
                revokingId = "worker-2",
                now = previewNow,
            ),
        )
    }
}

@Preview(widthDp = 390, heightDp = 844)
@Composable
private fun WorkersLoadingPreview() {
    QuokerTheme { WorkersContent(WorkersUiState(now = previewNow)) }
}

@Preview(widthDp = 390, heightDp = 844)
@Composable
private fun WorkersErrorPreview() {
    QuokerTheme { WorkersContent(WorkersUiState(error = QuokerError("forbidden", "", false), now = previewNow)) }
}

@Preview
@Composable
private fun AllowWorkerDialogPreview() {
    QuokerTheme {
        AllowWorkerDialog(AllowWorkerForm(id = "worker-2", fingerprint = "ab:cd"), {}, {}, {}, {})
    }
}
