package app.quoker.core.designsystem.component

import androidx.annotation.DrawableRes
import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawWithCache
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Shape
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import app.quoker.core.designsystem.R
import app.quoker.core.designsystem.icon.QuokerIcon
import app.quoker.core.designsystem.icon.QuokerIcons
import app.quoker.core.designsystem.theme.QuokerTheme

/** Shimmering placeholder block (`placeholder` color) for loading states. */
@Composable
fun SkeletonBlock(modifier: Modifier = Modifier, shape: Shape = RoundedCornerShape(8.dp)) {
    val c = QuokerTheme.colors
    val transition = rememberInfiniteTransition(label = "skeleton")
    val progress by transition.animateFloat(
        initialValue = -1f,
        targetValue = 2f,
        animationSpec = infiniteRepeatable(tween(1200, easing = LinearEasing), RepeatMode.Restart),
        label = "skeleton-shift",
    )
    Box(
        modifier
            .clip(shape)
            .drawWithCache {
                val width = size.width
                onDrawBehind {
                    drawRect(c.placeholder)
                    val x = width * progress
                    drawRect(
                        Brush.linearGradient(
                            colors = listOf(c.placeholder, c.outline, c.placeholder),
                            start = Offset(x - width / 2, 0f),
                            end = Offset(x + width / 2, 0f),
                        ),
                    )
                }
            },
    )
}

/** One line of skeleton text; [widthFraction] of the available width. */
@Composable
fun SkeletonLine(modifier: Modifier = Modifier, widthFraction: Float = 1f, height: Dp = 14.dp) {
    SkeletonBlock(modifier.fillMaxWidth(widthFraction).height(height), RoundedCornerShape(4.dp))
}

enum class BannerTone { Info, Error }

/**
 * Banner/alert card. Info: surface with blue icon and text (also «Ждём подтверждения…» with
 * [loading]); Error: errorContainer with a warning icon. Optional [action] text button.
 */
@Composable
fun Banner(
    text: String,
    modifier: Modifier = Modifier,
    tone: BannerTone = BannerTone.Info,
    title: String? = null,
    @DrawableRes icon: Int? = if (tone == BannerTone.Error) QuokerIcons.Warning else null,
    loading: Boolean = false,
    actionText: String? = null,
    onAction: (() -> Unit)? = null,
) {
    val c = QuokerTheme.colors
    val t = QuokerTheme.typography
    val bg = if (tone == BannerTone.Error) c.errorContainer else c.surface
    val iconColor = if (tone == BannerTone.Error) c.error else c.onSendContainer
    val titleColor = if (tone == BannerTone.Error) c.onErrorContainer else c.onSurface
    val textColor = when {
        tone == BannerTone.Error && title != null -> c.onErrorContainerVariant
        tone == BannerTone.Error -> c.onErrorContainer
        title != null -> c.onSurfaceStrong
        else -> c.onSendContainerStrong
    }
    Row(
        modifier = modifier
            .fillMaxWidth()
            .background(bg, RoundedCornerShape(if (title != null) 16.dp else 14.dp))
            .padding(horizontal = 16.dp, vertical = 14.dp)
            .semantics { liveRegion = LiveRegionMode.Polite },
        horizontalArrangement = Arrangement.spacedBy(12.dp),
        verticalAlignment = if (title != null) Alignment.Top else Alignment.CenterVertically,
    ) {
        when {
            loading -> CircularProgressIndicator(Modifier.size(18.dp), color = iconColor, strokeWidth = 2.2.dp)
            icon != null -> QuokerIcon(icon, contentDescription = null, tint = iconColor, size = 20.dp)
        }
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            if (title != null) {
                Text(title, style = t.bodyMedium.copy(fontWeight = FontWeight.W700), color = titleColor)
                Text(text, style = t.bodySmall, color = textColor)
            } else {
                Text(text, style = t.bodyMedium.copy(fontWeight = FontWeight.W600), color = textColor)
            }
        }
        if (actionText != null && onAction != null) {
            QuokerTextButton(actionText, onAction, contentPadding = PaddingValues(0.dp))
        }
    }
}

/** «Нет сети» banner, shared by screens. */
@Composable
fun NoNetworkBanner(modifier: Modifier = Modifier, onRetry: (() -> Unit)? = null) {
    Banner(
        text = stringResource(R.string.banner_no_network),
        modifier = modifier,
        tone = BannerTone.Error,
        actionText = onRetry?.let { stringResource(R.string.action_retry) },
        onAction = onRetry,
    )
}

enum class IconContainerTone { Error, Send, Download, Neutral }

/** Icon in a tinted circle/tile: error state (56 circle), Telegram onboarding (64, radius 20). */
@Composable
fun IconContainer(
    @DrawableRes icon: Int,
    modifier: Modifier = Modifier,
    tone: IconContainerTone = IconContainerTone.Error,
    size: Dp = 56.dp,
    shape: Shape = CircleShape,
    iconSize: Dp = 26.dp,
) {
    val c = QuokerTheme.colors
    val (bg, fg) = when (tone) {
        IconContainerTone.Error -> c.errorContainer to c.error
        IconContainerTone.Send -> c.send to c.onSend
        IconContainerTone.Download -> c.download to c.onDownload
        IconContainerTone.Neutral -> c.surface to c.onSurface
    }
    Box(modifier.size(size).background(bg, shape), contentAlignment = Alignment.Center) {
        QuokerIcon(icon, contentDescription = null, tint = fg, size = iconSize)
    }
}

/** Centered empty/info state: title, explanation and an optional action. */
@Composable
fun EmptyState(
    title: String,
    text: String,
    modifier: Modifier = Modifier,
    actionText: String? = null,
    onAction: (() -> Unit)? = null,
) {
    Column(
        modifier = modifier.fillMaxWidth().padding(vertical = 24.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text(title, style = QuokerTheme.typography.titleSmall, color = QuokerTheme.colors.onSurface)
        Text(
            text,
            style = QuokerTheme.typography.bodyMedium,
            color = QuokerTheme.colors.onSurfaceVariant,
            textAlign = TextAlign.Center,
        )
        if (actionText != null && onAction != null) {
            SecondaryButton(actionText, onAction, size = ButtonSize.Compact, modifier = Modifier.padding(top = 8.dp))
        }
    }
}

/** Confirmation dialog («Отвязать Telegram?», «Выйти?», «Заблокировать?»). */
@Composable
fun ConfirmDialog(
    title: String,
    text: String?,
    confirmText: String,
    onConfirm: () -> Unit,
    onDismiss: () -> Unit,
    dismissText: String = stringResource(R.string.action_cancel),
    destructive: Boolean = false,
) {
    val c = QuokerTheme.colors
    AlertDialog(
        onDismissRequest = onDismiss,
        containerColor = c.surface,
        titleContentColor = c.onSurface,
        textContentColor = c.onSurfaceStrong,
        tonalElevation = 0.dp,
        shape = RoundedCornerShape(28.dp),
        title = { Text(title, style = QuokerTheme.typography.titleMedium) },
        text = text?.let { { Text(it, style = QuokerTheme.typography.bodyParagraph) } },
        confirmButton = {
            QuokerTextButton(
                confirmText,
                onConfirm,
                tone = if (destructive) TextButtonTone.Danger else TextButtonTone.Accent,
                contentPadding = PaddingValues(horizontal = 12.dp, vertical = 12.dp),
            )
        },
        dismissButton = {
            QuokerTextButton(
                dismissText,
                onDismiss,
                tone = TextButtonTone.Plain,
                contentPadding = PaddingValues(horizontal = 12.dp, vertical = 12.dp),
            )
        },
    )
}

@Preview
@Composable
private fun FeedbackPreview() {
    QuokerTheme {
        Column(
            modifier = Modifier.background(QuokerTheme.colors.background).padding(20.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) {
                SkeletonBlock(Modifier.size(112.dp, 198.dp), RoundedCornerShape(14.dp))
                Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    SkeletonLine(widthFraction = 0.4f)
                    SkeletonLine()
                    SkeletonLine(widthFraction = 0.7f)
                }
            }
            Banner("Ждём подтверждения в Telegram…", loading = true)
            Banner(
                "3 ошибки login_required за час. Обновите cookies служебного аккаунта.",
                tone = BannerTone.Error,
                title = "Instagram требует вход",
            )
            NoNetworkBanner(onRetry = {})
            IconContainer(QuokerIcons.Error)
            IconContainer(QuokerIcons.Send, tone = IconContainerTone.Send, size = 64.dp, shape = RoundedCornerShape(20.dp), iconSize = 30.dp)
            EmptyState("Пока некуда отправлять", "Добавьте бота в группу или канал", actionText = "Обновить список", onAction = {})
        }
    }
}
