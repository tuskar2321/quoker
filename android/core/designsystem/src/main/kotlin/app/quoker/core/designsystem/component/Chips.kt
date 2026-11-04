package app.quoker.core.designsystem.component

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.graphics.PathEffect
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import app.quoker.core.designsystem.icon.QuokerIcon
import app.quoker.core.designsystem.icon.QuokerIcons
import app.quoker.core.designsystem.theme.QuokerTheme

/** «Вставить ссылку из буфера» chip: dashed `handle` outline, lime link icon, link preview below. */
@Composable
fun ClipboardChip(
    title: String,
    subtitle: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val c = QuokerTheme.colors
    val radius = 14.dp
    Row(
        modifier = modifier
            .fillMaxWidth()
            .defaultMinSize(minHeight = 48.dp)
            .clip(RoundedCornerShape(radius))
            .drawBehind {
                val stroke = 1.dp.toPx()
                drawRoundRect(
                    color = c.handle,
                    topLeft = androidx.compose.ui.geometry.Offset(stroke / 2, stroke / 2),
                    size = androidx.compose.ui.geometry.Size(size.width - stroke, size.height - stroke),
                    cornerRadius = CornerRadius(radius.toPx()),
                    style = Stroke(width = stroke, pathEffect = PathEffect.dashPathEffect(floatArrayOf(4.dp.toPx(), 3.dp.toPx()))),
                )
            }
            .clickable(role = Role.Button, onClick = onClick)
            .padding(horizontal = 14.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        QuokerIcon(QuokerIcons.Link, contentDescription = null, tint = c.download, size = 18.dp)
        Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(title, style = QuokerTheme.typography.bodyMedium.copy(fontWeight = FontWeight.W600), color = c.onSurface)
            Text(
                subtitle,
                style = QuokerTheme.typography.labelSmall.copy(fontWeight = FontWeight.W500),
                color = c.onSurfaceVariant,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
    }
}

/** Platform label in the video sheet («TikTok», «Reels»). */
@Composable
fun PlatformChip(text: String, modifier: Modifier = Modifier) {
    val c = QuokerTheme.colors
    Text(
        text = text,
        style = QuokerTheme.typography.labelSmall.copy(fontWeight = FontWeight.W700),
        color = c.onSurface,
        modifier = modifier
            .background(c.surfaceVariant, RoundedCornerShape(QuokerTheme.dimens.badgeRadius))
            .padding(horizontal = 10.dp, vertical = 4.dp),
    )
}

enum class BadgeTone { Download, Send, Error, Neutral }

/** Status badge: «Скачано» (Download), «→ Друзья» (Send), «Ошибка отправки» (Error). */
@Composable
fun QuokerBadge(text: String, tone: BadgeTone, modifier: Modifier = Modifier) {
    val c = QuokerTheme.colors
    val d = QuokerTheme.dimens
    val (bg, fg) = when (tone) {
        BadgeTone.Download -> c.downloadContainer to c.onDownloadContainer
        BadgeTone.Send -> c.sendContainer to c.onSendContainer
        BadgeTone.Error -> c.errorContainer to c.error
        BadgeTone.Neutral -> c.surfaceVariant to c.onSurfaceVariant
    }
    Text(
        text = text,
        style = QuokerTheme.typography.labelSmall,
        color = fg,
        maxLines = 1,
        overflow = TextOverflow.Ellipsis,
        modifier = modifier
            .background(bg, RoundedCornerShape(d.badgeRadius))
            .padding(horizontal = d.badgePaddingHorizontal, vertical = d.badgePaddingVertical),
    )
}

/** Duration over a cover («0:27»). */
@Composable
fun DurationBadge(text: String, modifier: Modifier = Modifier) {
    val c = QuokerTheme.colors
    Text(
        text = text,
        style = QuokerTheme.typography.labelSmall.copy(fontSize = 11.sp, fontWeight = FontWeight.W700),
        color = c.onSurface,
        modifier = modifier
            .background(c.background, RoundedCornerShape(6.dp))
            .padding(horizontal = 6.dp, vertical = 2.dp),
    )
}

/** Small status dot (online worker etc.). */
@Composable
fun StatusDot(modifier: Modifier = Modifier, active: Boolean = true) {
    val c = QuokerTheme.colors
    Box(modifier.size(8.dp).background(if (active) c.download else c.handle, RoundedCornerShape(4.dp)))
}

@Preview
@Composable
private fun ChipsPreview() {
    QuokerTheme {
        Column(
            modifier = Modifier.background(QuokerTheme.colors.background).padding(20.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            ClipboardChip("Вставить ссылку из буфера", "vm.tiktok.com/ZM…", {})
            PlatformChip("TikTok")
            Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                QuokerBadge("Скачано", BadgeTone.Download)
                QuokerBadge("→ Друзья", BadgeTone.Send)
                QuokerBadge("Ошибка отправки", BadgeTone.Error)
                QuokerBadge("Админ", BadgeTone.Neutral)
            }
            Row(horizontalArrangement = Arrangement.spacedBy(6.dp), verticalAlignment = Alignment.CenterVertically) {
                DurationBadge("0:27")
                StatusDot()
            }
        }
    }
}
