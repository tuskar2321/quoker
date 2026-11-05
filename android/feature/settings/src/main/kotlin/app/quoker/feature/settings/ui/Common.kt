package app.quoker.feature.settings.ui

import androidx.annotation.StringRes
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import app.quoker.core.common.QuokerError
import app.quoker.core.designsystem.component.Banner
import app.quoker.core.designsystem.component.BannerTone
import app.quoker.core.designsystem.component.EmptyState
import app.quoker.core.designsystem.component.ScreenHeader
import app.quoker.core.designsystem.component.SkeletonBlock
import app.quoker.core.designsystem.text.userMessage
import app.quoker.core.designsystem.theme.QuokerTheme
import app.quoker.feature.settings.R
import app.quoker.core.designsystem.R as DesignR

/** One-shot feedback shown as a dismissible banner at the top of a screen. */
sealed interface UiMessage {
    data class Failure(val error: QuokerError) : UiMessage
    data class ErrorText(@param:StringRes val text: Int) : UiMessage
    data class Info(@param:StringRes val text: Int) : UiMessage
}

@Composable
internal fun MessageBanner(message: UiMessage, onDismiss: () -> Unit, modifier: Modifier = Modifier) {
    val (text, tone) = when (message) {
        is UiMessage.Failure -> message.error.userMessage() to BannerTone.Error
        is UiMessage.ErrorText -> stringResource(message.text) to BannerTone.Error
        is UiMessage.Info -> stringResource(message.text) to BannerTone.Info
    }
    Banner(
        text = text,
        modifier = modifier,
        tone = tone,
        actionText = stringResource(DesignR.string.action_close),
        onAction = onDismiss,
    )
}

/** Back header + scrolling column with the 20dp gutter, as on every settings/admin mockup. */
@Composable
internal fun SettingsScaffold(
    title: String,
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
    message: UiMessage? = null,
    onDismissMessage: () -> Unit = {},
    spacing: androidx.compose.ui.unit.Dp = 10.dp,
    content: @Composable ColumnScope.() -> Unit,
) {
    Column(
        modifier
            .fillMaxSize()
            .background(QuokerTheme.colors.background)
            .statusBarsPadding(),
    ) {
        ScreenHeader(title = title, onBack = onBack)
        Column(
            Modifier
                .fillMaxWidth()
                .weight(1f)
                .verticalScroll(rememberScrollState())
                .navigationBarsPadding()
                .padding(start = 20.dp, end = 20.dp, top = 8.dp, bottom = 28.dp),
            verticalArrangement = Arrangement.spacedBy(spacing),
        ) {
            if (message != null) MessageBanner(message, onDismissMessage)
            content()
        }
    }
}

/** Full-screen load failure with «Повторить». */
@Composable
internal fun LoadError(error: QuokerError, onRetry: () -> Unit) {
    EmptyState(
        title = stringResource(R.string.admin_load_error_title),
        text = error.userMessage(),
        actionText = stringResource(DesignR.string.action_retry),
        onAction = onRetry,
    )
}

@Composable
internal fun CardSkeletons(count: Int = 3, height: androidx.compose.ui.unit.Dp = 68.dp) {
    repeat(count) {
        SkeletonBlock(Modifier.fillMaxWidth().height(height), RoundedCornerShape(16.dp))
    }
}

/** Standalone card row (users, workers): leading, two text lines, trailing action. */
@Composable
internal fun EntityCard(
    title: String,
    modifier: Modifier = Modifier,
    titleMuted: Boolean = false,
    leading: @Composable () -> Unit,
    subtitle: @Composable ColumnScope.() -> Unit,
    trailing: (@Composable RowScope.() -> Unit)? = null,
) {
    val c = QuokerTheme.colors
    Row(
        modifier
            .fillMaxWidth()
            .background(c.surface, RoundedCornerShape(QuokerTheme.dimens.cardRadius))
            .padding(PaddingValues(horizontal = 16.dp, vertical = 14.dp)),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        leading()
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(
                title,
                style = QuokerTheme.typography.bodyLarge.copy(fontWeight = FontWeight.W700),
                color = if (titleMuted) c.onSurfaceVariant else c.onSurface,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            subtitle()
        }
        trailing?.invoke(this)
    }
}

@Composable
internal fun CardSubtitle(text: String, error: Boolean = false) {
    Text(
        text,
        style = QuokerTheme.typography.labelSmall.copy(fontWeight = FontWeight.W500),
        color = if (error) QuokerTheme.colors.error else QuokerTheme.colors.onSurfaceVariant,
    )
}

@Composable
internal fun Note(text: String, modifier: Modifier = Modifier) {
    Text(
        text,
        modifier = modifier.padding(top = 6.dp),
        style = QuokerTheme.typography.bodySmall,
        color = QuokerTheme.colors.onSurfaceVariant,
    )
}
