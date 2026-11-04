package app.quoker.core.designsystem.component

import androidx.annotation.DrawableRes
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import app.quoker.core.designsystem.R
import app.quoker.core.designsystem.icon.QuokerIcon
import app.quoker.core.designsystem.icon.QuokerIcons
import app.quoker.core.designsystem.icon.QuokerLogo
import app.quoker.core.designsystem.theme.QuokerTheme

/** Round 48 icon button with a 24 icon (header actions, back). */
@Composable
fun QuokerIconButton(
    @DrawableRes icon: Int,
    contentDescription: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    tint: Color = QuokerTheme.colors.onSurface,
    size: Dp = 48.dp,
    iconSize: Dp = 24.dp,
    enabled: Boolean = true,
) {
    Box(
        modifier = modifier
            .size(size)
            .clip(CircleShape)
            .clickable(enabled = enabled, role = Role.Button, onClickLabel = contentDescription, onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        QuokerIcon(icon, contentDescription, tint = if (enabled) tint else QuokerTheme.colors.onSurfaceVariant, size = iconSize)
    }
}

/** Main screen header: frog 32 + «quoker», actions on the right (settings). */
@Composable
fun TopBar(
    modifier: Modifier = Modifier,
    actions: @Composable RowScope.() -> Unit = {},
) {
    Row(
        modifier = modifier.fillMaxWidth().padding(start = 20.dp, end = 12.dp, top = 20.dp, bottom = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Row(
            modifier = Modifier.weight(1f),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            QuokerLogo(size = QuokerTheme.dimens.logoTopBar)
            Text(
                stringResource(R.string.brand_name),
                style = QuokerTheme.typography.brand,
                color = QuokerTheme.colors.onSurface,
                modifier = Modifier.semantics { heading() },
            )
        }
        actions()
    }
}

/**
 * Back + title header (screens and the «Куда отправить» sheet). Screens use the default padding;
 * inside a sheet pass `PaddingValues(0.dp)` and offset the row by -12dp to align the arrow.
 */
@Composable
fun ScreenHeader(
    title: String,
    onBack: (() -> Unit)?,
    modifier: Modifier = Modifier,
    contentPadding: PaddingValues = PaddingValues(start = 4.dp, end = 12.dp, top = 16.dp, bottom = 8.dp),
    actions: @Composable RowScope.() -> Unit = {},
) {
    Row(
        modifier = modifier.fillMaxWidth().padding(contentPadding),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        if (onBack != null) {
            QuokerIconButton(QuokerIcons.Back, stringResource(R.string.action_back), onBack)
        }
        Text(
            title,
            style = QuokerTheme.typography.titleMedium,
            color = QuokerTheme.colors.onSurface,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
            modifier = Modifier.weight(1f).semantics { heading() },
        )
        actions()
    }
}

@Preview
@Composable
private fun BarsPreview() {
    QuokerTheme {
        Column(Modifier.background(QuokerTheme.colors.background)) {
            TopBar {
                QuokerIconButton(QuokerIcons.Settings, stringResource(R.string.action_settings), {})
            }
            ScreenHeader("История", onBack = {})
        }
    }
}
