package app.quoker.core.designsystem.component

import androidx.annotation.DrawableRes
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.LocalContentColor
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import app.quoker.core.designsystem.icon.QuokerIcon
import app.quoker.core.designsystem.icon.QuokerIcons
import app.quoker.core.designsystem.theme.QuokerTheme

/** Heights and radii seen in the mockups. Large is the main action size (56 / 16). */
enum class ButtonSize(val height: Dp, val radius: Dp, val horizontalPadding: Dp) {
    Large(56.dp, 16.dp, 20.dp),
    Medium(52.dp, 16.dp, 20.dp),
    Small(44.dp, 12.dp, 14.dp),
    Compact(40.dp, 12.dp, 14.dp),
    Mini(36.dp, 10.dp, 12.dp),
}

enum class PrimaryButtonVariant {
    /** Lime: download actions. */
    Download,

    /** Blue: Telegram actions. */
    Send,

    /** Light (onSurface) background: neutral main actions («Открыть видео», «Продолжить»). */
    Neutral,
}

/** Tone of outline and text buttons. */
enum class ButtonTone {
    Default,
    Danger,

    /** On a downloadContainer card (the new invite code card). */
    OnDownloadContainer,
}

@Composable
private fun buttonTextStyle(size: ButtonSize, strong: Boolean): TextStyle {
    val t = QuokerTheme.typography
    return when (size) {
        ButtonSize.Large -> if (strong) t.labelLarge else t.labelLarge.copy(fontWeight = FontWeight.W700)
        ButtonSize.Medium -> t.labelLarge.copy(fontSize = 15.sp, fontWeight = FontWeight.W700)
        ButtonSize.Small, ButtonSize.Compact ->
            t.labelLarge.copy(fontSize = 14.sp, fontWeight = if (strong) FontWeight.W700 else FontWeight.W600)
        ButtonSize.Mini -> t.labelMedium
    }
}

/**
 * Filled button. Full width by default for Large/Medium (pass a width modifier to override).
 * While [loading], shows a spinner and ignores clicks.
 */
@Composable
fun PrimaryButton(
    text: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    variant: PrimaryButtonVariant = PrimaryButtonVariant.Neutral,
    size: ButtonSize = ButtonSize.Large,
    @DrawableRes leadingIcon: Int? = null,
    enabled: Boolean = true,
    loading: Boolean = false,
    fullWidth: Boolean = size == ButtonSize.Large || size == ButtonSize.Medium,
) {
    val c = QuokerTheme.colors
    val (container, content) = when (variant) {
        PrimaryButtonVariant.Download -> c.download to c.onDownload
        PrimaryButtonVariant.Send -> c.send to c.onSend
        PrimaryButtonVariant.Neutral -> c.onSurface to c.background
    }
    val active = enabled && !loading
    val bg = if (enabled) container else c.surfaceVariant
    val fg = if (enabled) content else c.onSurfaceVariant
    ButtonFrame(
        onClick = onClick,
        enabled = active,
        size = size,
        fullWidth = fullWidth,
        modifier = modifier,
        background = bg,
        border = null,
        contentColor = fg,
    ) {
        if (loading) {
            CircularProgressIndicator(
                modifier = Modifier.size(if (size == ButtonSize.Large) 22.dp else 18.dp),
                color = fg,
                strokeWidth = 2.2.dp,
            )
        } else {
            ButtonContent(text, leadingIcon, buttonTextStyle(size, strong = true), size)
        }
    }
}

/** Outline button (border `outline`), e.g. «Закрыть», «Отвязать», «Скачать снова». */
@Composable
fun SecondaryButton(
    text: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    size: ButtonSize = ButtonSize.Large,
    tone: ButtonTone = ButtonTone.Default,
    @DrawableRes leadingIcon: Int? = null,
    enabled: Boolean = true,
    loading: Boolean = false,
    fullWidth: Boolean = size == ButtonSize.Large || size == ButtonSize.Medium,
) {
    val c = QuokerTheme.colors
    val (borderColor, content) = when (tone) {
        ButtonTone.Default -> c.outline to c.onSurface
        ButtonTone.Danger -> c.outline to c.error
        ButtonTone.OnDownloadContainer -> c.downloadProgress to c.onDownloadProgress
    }
    val fg = if (enabled) content else c.onSurfaceVariant
    ButtonFrame(
        onClick = onClick,
        enabled = enabled && !loading,
        size = size,
        fullWidth = fullWidth,
        modifier = modifier,
        background = Color.Transparent,
        border = BorderStroke(1.dp, borderColor),
        contentColor = fg,
    ) {
        if (loading) {
            CircularProgressIndicator(modifier = Modifier.size(18.dp), color = fg, strokeWidth = 2.dp)
        } else {
            ButtonContent(text, leadingIcon, buttonTextStyle(size, strong = false), size)
        }
    }
}

/** Borderless text action: links («Вся история», «Открыть оригинал») and «Отозвать». */
@Composable
fun QuokerTextButton(
    text: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    tone: TextButtonTone = TextButtonTone.Accent,
    enabled: Boolean = true,
    contentPadding: PaddingValues = PaddingValues(vertical = 12.dp),
) {
    val c = QuokerTheme.colors
    val color = when {
        !enabled -> c.onSurfaceVariant
        tone == TextButtonTone.Accent -> c.send
        tone == TextButtonTone.Danger -> c.error
        else -> c.onSurface
    }
    Box(
        modifier = modifier
            .defaultMinSize(minHeight = QuokerTheme.dimens.minTouchTarget)
            .clip(RoundedCornerShape(8.dp))
            .clickable(enabled = enabled, role = Role.Button, onClick = onClick)
            .padding(contentPadding),
        contentAlignment = Alignment.CenterStart,
    ) {
        Text(
            text = text,
            style = QuokerTheme.typography.bodyMedium.copy(
                fontWeight = if (tone == TextButtonTone.Danger) FontWeight.W700 else FontWeight.W600,
            ),
            color = color,
        )
    }
}

enum class TextButtonTone { Accent, Danger, Plain }

@Composable
internal fun ButtonFrame(
    onClick: () -> Unit,
    enabled: Boolean,
    size: ButtonSize,
    fullWidth: Boolean,
    modifier: Modifier,
    background: Color,
    border: BorderStroke?,
    contentColor: Color,
    content: @Composable () -> Unit,
) {
    val shape = RoundedCornerShape(size.radius)
    Box(
        modifier = modifier
            .then(if (fullWidth) Modifier.fillMaxWidth() else Modifier)
            .height(size.height)
            .clip(shape)
            .background(background, shape)
            .then(if (border != null) Modifier.border(border, shape) else Modifier)
            .clickable(enabled = enabled, role = Role.Button, onClick = onClick)
            .padding(horizontal = size.horizontalPadding),
        contentAlignment = Alignment.Center,
    ) {
        CompositionLocalProvider(LocalContentColor provides contentColor, content = content)
    }
}

@Composable
private fun ButtonContent(text: String, @DrawableRes icon: Int?, style: TextStyle, size: ButtonSize) {
    Row(
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        if (icon != null) {
            QuokerIcon(icon, contentDescription = null, size = if (size == ButtonSize.Large) 22.dp else 18.dp)
        }
        Text(text = text, style = style, color = LocalContentColor.current, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

@Preview
@Composable
private fun ButtonsPreview() {
    QuokerTheme {
        Column(
            modifier = Modifier.background(QuokerTheme.colors.surface).padding(20.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            PrimaryButton("Скачать", {}, variant = PrimaryButtonVariant.Download, leadingIcon = QuokerIcons.Download)
            PrimaryButton("Отправить в Telegram", {}, variant = PrimaryButtonVariant.Send, leadingIcon = QuokerIcons.Send)
            PrimaryButton("Открыть видео", {}, size = ButtonSize.Medium)
            PrimaryButton("Загрузка", {}, loading = true)
            PrimaryButton("Недоступно", {}, enabled = false)
            SecondaryButton("Закрыть", {})
            SecondaryButton("Выйти", {}, size = ButtonSize.Medium, tone = ButtonTone.Danger)
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                SecondaryButton("Скачать снова", {}, size = ButtonSize.Mini)
                SecondaryButton("Отвязать", {}, size = ButtonSize.Compact, tone = ButtonTone.Danger)
                PrimaryButton("Загрузить", {}, size = ButtonSize.Compact)
            }
            QuokerTextButton("Вся история", {})
            QuokerTextButton("Отозвать", {}, tone = TextButtonTone.Danger)
        }
    }
}
