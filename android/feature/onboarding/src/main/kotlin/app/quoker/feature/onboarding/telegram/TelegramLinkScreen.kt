package app.quoker.feature.onboarding.telegram

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.compose.LifecycleStartEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import app.quoker.core.common.QuokerError
import app.quoker.core.designsystem.component.Banner
import app.quoker.core.designsystem.component.BannerTone
import app.quoker.core.designsystem.component.IconContainer
import app.quoker.core.designsystem.component.IconContainerTone
import app.quoker.core.designsystem.component.PrimaryButton
import app.quoker.core.designsystem.component.PrimaryButtonVariant
import app.quoker.core.designsystem.component.QuokerIconButton
import app.quoker.core.designsystem.component.QuokerTextButton
import app.quoker.core.designsystem.component.TextButtonTone
import app.quoker.core.designsystem.icon.QuokerIcons
import app.quoker.core.designsystem.text.userMessage
import app.quoker.core.designsystem.theme.QuokerTheme
import app.quoker.feature.onboarding.R
import app.quoker.core.designsystem.R as DesignR

/**
 * Stateful Telegram binding screen (onboarding, Settings, the video sheet's «Нет Telegram»).
 * Polls the binding status every 2 s only while the screen is visible.
 *
 * @param showSkip shows «Пропустить» ([onSkip]); otherwise a back arrow ([onBack]).
 */
@Composable
fun TelegramLinkScreen(
    showSkip: Boolean,
    onLinked: () -> Unit,
    onSkip: () -> Unit,
    onBack: () -> Unit,
    viewModel: TelegramLinkViewModel = hiltViewModel(),
) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    val context = LocalContext.current
    val linked by rememberUpdatedState(onLinked)

    LifecycleStartEffect(viewModel) {
        viewModel.onVisible()
        onStopOrDispose { viewModel.onHidden() }
    }
    LaunchedEffect(viewModel) {
        viewModel.events.collect { event ->
            when (event) {
                is TelegramLinkEvent.OpenLink -> if (!context.openUrl(event.url)) viewModel.onOpenFailed()
                TelegramLinkEvent.Linked -> linked()
            }
        }
    }
    TelegramLinkContent(
        state = state,
        showSkip = showSkip,
        onOpenBot = viewModel::onOpenBot,
        onRenew = viewModel::onRenew,
        onRetry = viewModel::onRetry,
        onSkip = onSkip,
        onBack = onBack,
    )
}

/** ACTION_VIEW on the deep link: Telegram if installed, otherwise the browser. */
private fun Context.openUrl(url: String): Boolean = try {
    startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url)))
    true
} catch (_: ActivityNotFoundException) {
    false
}

@Composable
fun TelegramLinkContent(
    state: TelegramLinkUiState,
    showSkip: Boolean,
    onOpenBot: () -> Unit,
    onRenew: () -> Unit,
    onRetry: () -> Unit,
    onSkip: () -> Unit,
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val c = QuokerTheme.colors
    val t = QuokerTheme.typography
    val gutter = QuokerTheme.dimens.onboardingGutter
    Box(modifier.fillMaxSize().background(c.background)) {
        Column(Modifier.fillMaxSize().safeDrawingPadding()) {
            // The 72dp top inset of the mockup holds the back arrow or «Пропустить».
            Row(
                modifier = Modifier.fillMaxWidth().height(56.dp).padding(horizontal = 8.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                if (!showSkip) {
                    QuokerIconButton(QuokerIcons.Back, stringResource(DesignR.string.action_back), onBack)
                }
                Spacer(Modifier.weight(1f))
                if (showSkip) {
                    QuokerTextButton(
                        stringResource(R.string.telegram_skip),
                        onSkip,
                        tone = TextButtonTone.Plain,
                        contentPadding = PaddingValues(horizontal = 16.dp, vertical = 12.dp),
                    )
                }
            }
            Column(
                modifier = Modifier.fillMaxSize().padding(start = gutter, end = gutter, top = 16.dp, bottom = 32.dp),
                verticalArrangement = Arrangement.spacedBy(20.dp),
            ) {
                IconContainer(
                    icon = QuokerIcons.Send,
                    tone = IconContainerTone.Send,
                    size = 64.dp,
                    shape = RoundedCornerShape(20.dp),
                    iconSize = 30.dp,
                )
                Text(
                    stringResource(R.string.telegram_title),
                    style = t.displaySmall,
                    color = c.onSurface,
                    modifier = Modifier.padding(top = 8.dp).semantics { heading() },
                )
                Text(stringResource(R.string.telegram_text), style = t.bodyParagraph, color = c.onSurfaceStrong)
                Column(Modifier.padding(top = 4.dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
                    Step(1, stringResource(R.string.telegram_step_start))
                    Step(2, stringResource(R.string.telegram_step_add))
                }
                StatusBlock(state, onRenew, onRetry)
                Spacer(Modifier.weight(1f))
                Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                    PrimaryButton(
                        text = stringResource(R.string.telegram_open_bot),
                        onClick = onOpenBot,
                        variant = PrimaryButtonVariant.Send,
                        leadingIcon = QuokerIcons.Send,
                        loading = state.creatingLink,
                    )
                    Text(
                        stringResource(R.string.telegram_link_ttl),
                        style = t.bodySmall,
                        color = c.onSurfaceVariant,
                        textAlign = TextAlign.Center,
                        modifier = Modifier.fillMaxWidth(),
                    )
                }
            }
        }
    }
}

@Composable
private fun StatusBlock(state: TelegramLinkUiState, onRenew: () -> Unit, onRetry: () -> Unit) {
    when {
        state.error != null -> Banner(
            text = state.error.userMessage(),
            tone = BannerTone.Error,
            actionText = stringResource(DesignR.string.action_retry),
            onAction = onRetry,
        )
        state.openFailed -> Banner(text = stringResource(R.string.telegram_open_failed), tone = BannerTone.Error)
        state.phase == TelegramLinkPhase.Expired -> Banner(
            text = stringResource(R.string.telegram_expired),
            tone = BannerTone.Error,
            icon = QuokerIcons.Refresh,
            modifier = Modifier.clip(RoundedCornerShape(14.dp)).clickable(role = Role.Button, enabled = !state.creatingLink, onClick = onRenew),
        )
        state.phase == TelegramLinkPhase.Waiting -> Banner(text = stringResource(R.string.telegram_waiting), loading = true)
        else -> Unit
    }
}

@Composable
private fun Step(number: Int, text: String) {
    val c = QuokerTheme.colors
    Row(horizontalArrangement = Arrangement.spacedBy(14.dp), verticalAlignment = Alignment.Top) {
        Box(Modifier.size(28.dp).background(c.sendContainer, CircleShape), contentAlignment = Alignment.Center) {
            Text(
                number.toString(),
                style = QuokerTheme.typography.bodyMedium.copy(fontWeight = FontWeight.W800),
                color = c.onSendContainer,
            )
        }
        Text(
            text,
            style = QuokerTheme.typography.bodyParagraph.copy(lineHeight = (15 * 1.45f).sp),
            color = c.onSurface,
            modifier = Modifier.padding(top = 3.dp),
        )
    }
}

@Composable
private fun PreviewScreen(state: TelegramLinkUiState, showSkip: Boolean = true) {
    QuokerTheme { TelegramLinkContent(state, showSkip, {}, {}, {}, {}, {}) }
}

@Preview(widthDp = 390, heightDp = 844)
@Composable
private fun TelegramIdlePreview() = PreviewScreen(TelegramLinkUiState())

@Preview(widthDp = 390, heightDp = 844)
@Composable
private fun TelegramWaitingPreview() = PreviewScreen(TelegramLinkUiState(phase = TelegramLinkPhase.Waiting))

@Preview(widthDp = 390, heightDp = 844)
@Composable
private fun TelegramCreatingPreview() =
    PreviewScreen(TelegramLinkUiState(creatingLink = true), showSkip = false)

@Preview(widthDp = 390, heightDp = 844)
@Composable
private fun TelegramExpiredPreview() = PreviewScreen(TelegramLinkUiState(phase = TelegramLinkPhase.Expired))

@Preview(widthDp = 390, heightDp = 844)
@Composable
private fun TelegramErrorPreview() = PreviewScreen(
    TelegramLinkUiState(phase = TelegramLinkPhase.Waiting, error = QuokerError("network", "", true)),
    showSkip = false,
)
