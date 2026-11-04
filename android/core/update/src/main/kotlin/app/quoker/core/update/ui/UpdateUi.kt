package app.quoker.core.update.ui

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.DialogProperties
import app.quoker.core.common.QuokerError
import app.quoker.core.designsystem.component.IconContainer
import app.quoker.core.designsystem.component.IconContainerTone
import app.quoker.core.designsystem.component.PrimaryButton
import app.quoker.core.designsystem.component.QuokerProgressBar
import app.quoker.core.designsystem.component.QuokerTextButton
import app.quoker.core.designsystem.component.TextButtonTone
import app.quoker.core.designsystem.icon.QuokerIcons
import app.quoker.core.designsystem.text.userMessage
import app.quoker.core.designsystem.theme.QuokerTheme
import app.quoker.core.update.R
import app.quoker.core.update.UpdateErrorCodes
import app.quoker.core.update.UpdateInfo
import app.quoker.core.update.UpdateState

/**
 * Optional-update dialog. Show it while `state` is not [UpdateState.None] and not `required`.
 * [onUpdate] → `UpdateManager.proceed(activity)`, [onLater] → `UpdateManager.dismiss()`.
 */
@Composable
fun UpdateDialog(state: UpdateState, onUpdate: () -> Unit, onLater: () -> Unit) {
    if (state is UpdateState.None || state.required) return
    val c = QuokerTheme.colors
    AlertDialog(
        onDismissRequest = onLater,
        properties = DialogProperties(dismissOnClickOutside = state !is UpdateState.Downloading),
        containerColor = c.surface,
        titleContentColor = c.onSurface,
        textContentColor = c.onSurfaceStrong,
        tonalElevation = 0.dp,
        shape = RoundedCornerShape(QuokerTheme.dimens.sheetRadius),
        title = {
            Text(
                stringResource(
                    if (state is UpdateState.Failed) R.string.update_failed_title else R.string.update_available_title,
                ),
                style = QuokerTheme.typography.titleMedium,
            )
        },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                state.info?.let { VersionAndChangelog(it, Modifier.heightIn(max = 280.dp)) }
                UpdateProgress(state)
            }
        },
        confirmButton = {
            if (state !is UpdateState.Downloading) {
                QuokerTextButton(
                    actionText(state),
                    onUpdate,
                    tone = TextButtonTone.Accent,
                    contentPadding = DialogButtonPadding,
                )
            }
        },
        dismissButton = {
            QuokerTextButton(
                stringResource(R.string.update_action_later),
                onLater,
                tone = TextButtonTone.Plain,
                contentPadding = DialogButtonPadding,
            )
        },
    )
}

/** Blocking «Обновите приложение» screen for `state.required`; back is swallowed. */
@Composable
fun UpgradeRequiredScreen(state: UpdateState, onUpdate: () -> Unit, modifier: Modifier = Modifier) {
    BackHandler(enabled = true) {}
    val c = QuokerTheme.colors
    val d = QuokerTheme.dimens
    Column(
        modifier = modifier
            .fillMaxSize()
            .background(c.background)
            .safeDrawingPadding()
            .padding(horizontal = d.onboardingGutter, vertical = d.sectionGap),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Spacer(Modifier.weight(1f))
        IconContainer(QuokerIcons.Refresh, tone = IconContainerTone.Download)
        Spacer(Modifier.height(d.sheetGap))
        Text(
            stringResource(R.string.update_required_title),
            style = QuokerTheme.typography.titleLarge,
            color = c.onSurface,
            textAlign = TextAlign.Center,
        )
        Spacer(Modifier.height(d.buttonGap))
        Text(
            stringResource(R.string.update_required_text),
            style = QuokerTheme.typography.bodyParagraph,
            color = c.onSurfaceVariant,
            textAlign = TextAlign.Center,
        )
        state.info?.let {
            Spacer(Modifier.height(d.sectionGap))
            VersionAndChangelog(it, Modifier.weight(1f, fill = false))
        }
        Spacer(Modifier.weight(1f))
        Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(d.buttonGap)) {
            UpdateProgress(state)
            PrimaryButton(
                text = actionText(state),
                onClick = onUpdate,
                loading = state is UpdateState.Downloading,
                enabled = state !is UpdateState.Downloading,
            )
        }
    }
}

@Composable
private fun VersionAndChangelog(info: UpdateInfo, modifier: Modifier = Modifier) {
    val c = QuokerTheme.colors
    Column(
        modifier = modifier.fillMaxWidth().verticalScroll(rememberScrollState()),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Text(
            stringResource(R.string.update_version, info.versionName),
            style = QuokerTheme.typography.labelLarge,
            color = c.onSurface,
        )
        if (info.changelog.isNotBlank()) {
            Text(
                stringResource(R.string.update_changelog_title),
                style = QuokerTheme.typography.labelMedium,
                color = c.onSurfaceVariant,
            )
            Text(info.changelog, style = QuokerTheme.typography.bodyParagraph, color = c.onSurfaceStrong)
        }
    }
}

@Composable
private fun UpdateProgress(state: UpdateState) {
    val c = QuokerTheme.colors
    when (state) {
        is UpdateState.Downloading -> {
            Text(
                stringResource(R.string.update_downloading, state.percent),
                style = QuokerTheme.typography.bodySmall,
                color = c.onSurfaceVariant,
            )
            QuokerProgressBar(state.percent / 100f, color = c.download)
        }
        is UpdateState.ReadyToInstall -> Text(
            stringResource(R.string.update_ready),
            style = QuokerTheme.typography.bodySmall,
            color = c.onSurfaceVariant,
        )
        is UpdateState.Failed -> Text(
            updateErrorMessage(state.error),
            style = QuokerTheme.typography.bodySmall,
            color = c.error,
        )
        else -> Unit
    }
}

@Composable
private fun actionText(state: UpdateState): String = stringResource(
    when (state) {
        is UpdateState.ReadyToInstall -> R.string.update_action_install
        else -> R.string.update_action_update
    },
)

/** Text for errors of the update flow: this module's codes first, then the shared `error_<code>`. */
@Composable
fun updateErrorMessage(error: QuokerError): String = when (error.code) {
    UpdateErrorCodes.CHECKSUM_MISMATCH -> stringResource(R.string.update_error_checksum)
    UpdateErrorCodes.DOWNLOAD_FAILED -> stringResource(R.string.update_error_download)
    UpdateErrorCodes.INSTALL_FAILED -> stringResource(R.string.update_error_install)
    UpdateErrorCodes.NO_RELEASE -> stringResource(R.string.update_error_no_release)
    else -> error.userMessage()
}

private val DialogButtonPadding = PaddingValues(horizontal = 12.dp, vertical = 12.dp)

private val PreviewInfo = UpdateInfo(42, "1.4.0", "— Отправка в каналы\n— Исправлены ошибки")

@Preview
@Composable
private fun UpdateDialogPreview() {
    QuokerTheme { UpdateDialog(UpdateState.Downloading(37, PreviewInfo, required = false), {}, {}) }
}

@Preview(widthDp = 390, heightDp = 844)
@Composable
private fun UpgradeRequiredPreview() {
    QuokerTheme { UpgradeRequiredScreen(UpdateState.Required(PreviewInfo), {}) }
}
