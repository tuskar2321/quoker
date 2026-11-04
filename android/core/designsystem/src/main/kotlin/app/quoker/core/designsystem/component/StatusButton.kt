package app.quoker.core.designsystem.component

import androidx.annotation.DrawableRes
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import app.quoker.core.designsystem.R
import app.quoker.core.designsystem.icon.QuokerIcon
import app.quoker.core.designsystem.icon.QuokerIcons
import app.quoker.core.designsystem.theme.QuokerTheme

enum class StatusTone {
    /** Download in progress / saved («Скачивается · 62%», «Сохранено · Открыть»). */
    Download,

    /** Telegram status («Отправляется…», «Отправлено в „X“ · Ещё»). */
    Send,

    /** Failure («Не отправлено · Повторить»). */
    Error,
}

private data class StatusColors(val container: Color, val fill: Color, val icon: Color, val text: Color, val action: Color)

@Composable
private fun statusColors(tone: StatusTone): StatusColors {
    val c = QuokerTheme.colors
    return when (tone) {
        StatusTone.Download -> StatusColors(c.downloadContainer, c.downloadProgress, c.onDownloadContainer, c.onDownloadProgress, c.download)
        StatusTone.Send -> StatusColors(c.sendContainer, c.sendContainer, c.onSendContainer, c.onSendContainerStrong, c.send)
        StatusTone.Error -> StatusColors(c.errorContainer, c.errorContainer, c.error, c.onErrorContainer, c.error)
    }
}

/**
 * The 56-high status bar that replaces an action button while it runs or after it finished
 * (Downloading.dc.html). [progress] in 0..1 draws the fill; [loading] shows a spinner instead of
 * the icon; [trailing] holds [StatusButtonAction] / [StatusButtonIconAction].
 */
@Composable
fun StatusButton(
    text: String,
    tone: StatusTone,
    modifier: Modifier = Modifier,
    @DrawableRes icon: Int? = null,
    progress: Float? = null,
    loading: Boolean = false,
    onClick: (() -> Unit)? = null,
    trailing: (@Composable RowScope.() -> Unit)? = null,
) {
    val colors = statusColors(tone)
    val shape = RoundedCornerShape(QuokerTheme.dimens.buttonRadius)
    val animated by animateFloatAsState((progress ?: 0f).coerceIn(0f, 1f), label = "status-progress")
    Box(
        modifier = modifier
            .fillMaxWidth()
            .height(QuokerTheme.dimens.buttonHeight)
            .clip(shape)
            .background(colors.container, shape)
            .then(if (onClick != null) Modifier.clickable(role = Role.Button, onClick = onClick) else Modifier)
            .semantics { liveRegion = LiveRegionMode.Polite },
    ) {
        if (progress != null) {
            Box(Modifier.fillMaxHeight().fillMaxWidth(animated).background(colors.fill))
        }
        Row(
            modifier = Modifier.fillMaxWidth().fillMaxHeight().padding(start = 16.dp, end = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            when {
                loading -> CircularProgressIndicator(Modifier.size(20.dp), color = colors.icon, strokeWidth = 2.2.dp)
                icon != null -> QuokerIcon(icon, contentDescription = null, tint = colors.icon, size = 22.dp)
            }
            Text(
                text,
                style = QuokerTheme.typography.bodyLarge.copy(fontWeight = FontWeight.W700),
                color = colors.text,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
                modifier = Modifier.weight(1f),
            )
            trailing?.invoke(this)
        }
    }
}

/** Text action at the end of a [StatusButton] («Ещё», «Открыть», «Повторить»). */
@Composable
fun StatusButtonAction(text: String, tone: StatusTone, onClick: () -> Unit, modifier: Modifier = Modifier) {
    Box(
        modifier = modifier
            .height(44.dp)
            .clip(RoundedCornerShape(12.dp))
            .clickable(role = Role.Button, onClick = onClick)
            .padding(horizontal = 10.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(
            text,
            style = QuokerTheme.typography.bodyMedium.copy(fontWeight = FontWeight.W700, fontSize = 14.sp),
            color = statusColors(tone).action,
        )
    }
}

/** Icon action at the end of a [StatusButton] (cancel). */
@Composable
fun StatusButtonIconAction(
    @DrawableRes icon: Int,
    contentDescription: String,
    tone: StatusTone,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Box(
        modifier = modifier
            .size(44.dp)
            .clip(RoundedCornerShape(12.dp))
            .clickable(role = Role.Button, onClickLabel = contentDescription, onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        QuokerIcon(icon, contentDescription, tint = statusColors(tone).text, size = 20.dp)
    }
}

/** «Скачивается · N%» with the progress fill and a cancel button. */
@Composable
fun DownloadProgressButton(percent: Int, onCancel: () -> Unit, modifier: Modifier = Modifier) {
    val p = percent.coerceIn(0, 100)
    StatusButton(
        text = stringResource(R.string.download_progress, p),
        tone = StatusTone.Download,
        modifier = modifier,
        icon = QuokerIcons.Download,
        progress = p / 100f,
        trailing = {
            StatusButtonIconAction(QuokerIcons.Close, stringResource(R.string.download_cancel), StatusTone.Download, onCancel)
        },
    )
}

@Preview
@Composable
private fun StatusButtonPreview() {
    QuokerTheme {
        Column(
            modifier = Modifier.background(QuokerTheme.colors.surface).padding(20.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            DownloadProgressButton(62, {})
            StatusButton("Сохранено", StatusTone.Download, icon = QuokerIcons.Check, trailing = {
                StatusButtonAction("Открыть", StatusTone.Download, {})
            })
            StatusButton("Отправляется в «Друзья»…", StatusTone.Send, loading = true)
            StatusButton("Отправлено в «Друзья»", StatusTone.Send, icon = QuokerIcons.Check, trailing = {
                StatusButtonAction("Ещё", StatusTone.Send, {})
            })
            StatusButton("Не отправлено", StatusTone.Error, icon = QuokerIcons.Error, trailing = {
                StatusButtonAction("Повторить", StatusTone.Error, {})
            })
        }
    }
}
