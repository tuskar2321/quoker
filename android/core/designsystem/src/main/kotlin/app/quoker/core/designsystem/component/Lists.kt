package app.quoker.core.designsystem.component

import androidx.annotation.DrawableRes
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CheckboxDefaults
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.RadioButton
import androidx.compose.material3.RadioButtonDefaults
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import app.quoker.core.designsystem.icon.QuokerIcon
import app.quoker.core.designsystem.icon.QuokerIcons
import app.quoker.core.designsystem.theme.QuokerTheme

/** Overline group header («СЕГОДНЯ», «TELEGRAM»), upper-cased here; optional trailing [action]. */
@Composable
fun SectionHeader(
    text: String,
    modifier: Modifier = Modifier,
    action: (@Composable RowScope.() -> Unit)? = null,
) {
    Row(
        modifier = modifier.fillMaxWidth().defaultMinSize(minHeight = if (action != null) 44.dp else 0.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = text.uppercase(),
            style = QuokerTheme.typography.overline,
            color = QuokerTheme.colors.onSurfaceVariant,
            modifier = Modifier.weight(1f).semantics { heading() },
        )
        action?.invoke(this)
    }
}

/** Card: surface, radius 16. */
@Composable
fun QuokerCard(
    modifier: Modifier = Modifier,
    color: Color = QuokerTheme.colors.surface,
    contentPadding: PaddingValues = PaddingValues(horizontal = 16.dp, vertical = 14.dp),
    onClick: (() -> Unit)? = null,
    content: @Composable ColumnScope.() -> Unit,
) {
    val shape = RoundedCornerShape(QuokerTheme.dimens.cardRadius)
    Column(
        modifier = modifier
            .fillMaxWidth()
            .clip(shape)
            .background(color, shape)
            .then(if (onClick != null) Modifier.clickable(onClick = onClick) else Modifier)
            .padding(contentPadding),
        content = content,
    )
}

/** Settings-style group: surface card (radius 16, padding 4×16) holding [ListRow]s. */
@Composable
fun ListGroup(
    modifier: Modifier = Modifier,
    header: String? = null,
    headerAction: (@Composable RowScope.() -> Unit)? = null,
    content: @Composable ColumnScope.() -> Unit,
) {
    Column(modifier = modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        if (header != null) SectionHeader(header, action = headerAction)
        QuokerCard(contentPadding = PaddingValues(horizontal = 16.dp, vertical = 4.dp), content = content)
    }
}

enum class RowTextTone { Default, Muted, Error, Accent }

/**
 * Row inside a [ListGroup]. [divider] draws the 1dp divider below (all rows but the last).
 * Muted titles are for inactive items; [subtitleTone] Error for reasons like «Бот удалён из канала».
 */
@Composable
fun ListRow(
    title: String,
    modifier: Modifier = Modifier,
    subtitle: String? = null,
    titleTone: RowTextTone = RowTextTone.Default,
    subtitleTone: RowTextTone = RowTextTone.Muted,
    leading: (@Composable () -> Unit)? = null,
    trailing: (@Composable RowScope.() -> Unit)? = null,
    onClick: (() -> Unit)? = null,
    enabled: Boolean = true,
    divider: Boolean = false,
    minHeight: Dp = QuokerTheme.dimens.listRowTallMinHeight,
) {
    val t = QuokerTheme.typography
    Column(modifier = modifier.fillMaxWidth()) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .defaultMinSize(minHeight = minHeight)
                .then(if (onClick != null) Modifier.clickable(enabled = enabled, onClick = onClick) else Modifier)
                .padding(vertical = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            leading?.invoke()
            Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
                Text(
                    title,
                    style = t.bodyLarge,
                    color = toneColor(titleTone, default = QuokerTheme.colors.onSurface),
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
                if (subtitle != null) {
                    Text(subtitle, style = t.labelSmall.copy(fontWeight = FontWeight.W500), color = toneColor(subtitleTone))
                }
            }
            trailing?.invoke(this)
        }
        if (divider) HorizontalDivider(thickness = 1.dp, color = QuokerTheme.colors.divider)
    }
}

/** Chevron for navigable rows. */
@Composable
fun ListRowChevron() {
    QuokerIcon(QuokerIcons.ChevronRight, contentDescription = null, tint = QuokerTheme.colors.onSurfaceVariant, size = 18.dp)
}

/**
 * Selectable card row (send target): selected = sendContainer with a 1.5 `send` border,
 * otherwise surfaceVariant. Disabled rows are dimmed.
 */
@Composable
fun SelectableRow(
    selected: Boolean,
    onClick: () -> Unit,
    title: String,
    modifier: Modifier = Modifier,
    subtitle: String? = null,
    enabled: Boolean = true,
    leading: (@Composable () -> Unit)? = null,
    trailing: (@Composable RowScope.() -> Unit)? = { QuokerRadioButton(selected = selected, onClick = null, enabled = enabled) },
) {
    val c = QuokerTheme.colors
    val shape = RoundedCornerShape(16.dp)
    Row(
        modifier = modifier
            .fillMaxWidth()
            .defaultMinSize(minHeight = 64.dp)
            .clip(shape)
            .background(if (selected) c.sendContainer else c.surfaceVariant, shape)
            .border(1.5.dp, if (selected) c.send else c.surfaceVariant, shape)
            .selectable(selected = selected, enabled = enabled, role = Role.RadioButton, onClick = onClick)
            .padding(horizontal = 16.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        leading?.invoke()
        Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(2.dp)) {
            Text(
                title,
                style = QuokerTheme.typography.bodyLarge.copy(fontWeight = FontWeight.W700),
                color = if (enabled) c.onSurface else c.onSurfaceVariant,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            if (subtitle != null) {
                Text(
                    subtitle,
                    style = QuokerTheme.typography.labelSmall.copy(fontWeight = FontWeight.W500),
                    color = if (selected) c.onSendContainerVariant else c.onSurfaceVariant,
                )
            }
        }
        trailing?.invoke(this)
    }
}

enum class AvatarTone { Neutral, Send, SendSolid, Download, Error }

/** Round initial/icon avatar (targets, users, Telegram account). */
@Composable
fun InitialAvatar(
    initial: String,
    modifier: Modifier = Modifier,
    tone: AvatarTone = AvatarTone.Neutral,
    size: Dp = QuokerTheme.dimens.avatar,
    @DrawableRes icon: Int? = null,
) {
    val c = QuokerTheme.colors
    val (bg, fg) = when (tone) {
        AvatarTone.Neutral -> c.placeholder to c.onSurface
        AvatarTone.Send -> c.sendContainer to c.onSendContainer
        AvatarTone.SendSolid -> c.send to c.onSend
        AvatarTone.Download -> c.downloadContainer to c.onDownloadContainer
        AvatarTone.Error -> c.errorContainer to c.error
    }
    Box(
        modifier = modifier.size(size).background(bg, CircleShape),
        contentAlignment = Alignment.Center,
    ) {
        if (icon != null) {
            QuokerIcon(icon, contentDescription = null, tint = fg, size = 18.dp)
        } else {
            Text(
                initial.take(1).uppercase(),
                style = QuokerTheme.typography.bodyLarge.copy(
                    fontWeight = FontWeight.W800,
                    fontSize = if (size >= 44.dp) 17.sp else 15.sp,
                ),
                color = fg,
            )
        }
    }
}

@Composable
fun QuokerSwitch(
    checked: Boolean,
    onCheckedChange: ((Boolean) -> Unit)?,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
) {
    val c = QuokerTheme.colors
    Switch(
        checked = checked,
        onCheckedChange = onCheckedChange,
        modifier = modifier,
        enabled = enabled,
        colors = SwitchDefaults.colors(
            checkedThumbColor = c.onSend,
            checkedTrackColor = c.send,
            checkedBorderColor = c.send,
            uncheckedThumbColor = c.onSurfaceVariant,
            uncheckedTrackColor = c.surfaceVariant,
            uncheckedBorderColor = c.outline,
            disabledCheckedTrackColor = c.placeholder,
            disabledCheckedThumbColor = c.handle,
            disabledUncheckedTrackColor = c.surface,
            disabledUncheckedThumbColor = c.handle,
            disabledUncheckedBorderColor = c.outline,
        ),
    )
}

@Composable
fun QuokerCheckbox(
    checked: Boolean,
    onCheckedChange: ((Boolean) -> Unit)?,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
) {
    val c = QuokerTheme.colors
    Checkbox(
        checked = checked,
        onCheckedChange = onCheckedChange,
        modifier = modifier,
        enabled = enabled,
        colors = CheckboxDefaults.colors(
            checkedColor = c.send,
            uncheckedColor = c.handle,
            checkmarkColor = c.onSend,
            disabledCheckedColor = c.placeholder,
            disabledUncheckedColor = c.outline,
        ),
    )
}

@Composable
fun QuokerRadioButton(
    selected: Boolean,
    onClick: (() -> Unit)?,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
) {
    val c = QuokerTheme.colors
    RadioButton(
        selected = selected,
        onClick = onClick,
        modifier = modifier,
        enabled = enabled,
        colors = RadioButtonDefaults.colors(
            selectedColor = c.send,
            unselectedColor = c.handle,
            disabledSelectedColor = c.placeholder,
            disabledUnselectedColor = c.outline,
        ),
    )
}

/** Two-or-more option toggle (role picker in invites): surface track, placeholder thumb. */
@Composable
fun SegmentedControl(
    options: List<String>,
    selectedIndex: Int,
    onSelect: (Int) -> Unit,
    modifier: Modifier = Modifier,
) {
    val c = QuokerTheme.colors
    Row(
        modifier = modifier
            .fillMaxWidth()
            .background(c.surface, RoundedCornerShape(14.dp))
            .padding(4.dp),
    ) {
        options.forEachIndexed { index, label ->
            val selected = index == selectedIndex
            Box(
                modifier = Modifier
                    .weight(1f)
                    .height(40.dp)
                    .clip(RoundedCornerShape(10.dp))
                    .background(if (selected) c.placeholder else Color.Transparent)
                    .selectable(selected = selected, role = Role.RadioButton, onClick = { onSelect(index) }),
                contentAlignment = Alignment.Center,
            ) {
                Text(
                    label,
                    style = QuokerTheme.typography.bodyMedium.copy(fontWeight = if (selected) FontWeight.W700 else FontWeight.W600),
                    color = if (selected) c.onSurface else c.onSurfaceVariant,
                )
            }
        }
    }
}

/** Thin 4dp meter (disk usage). */
@Composable
fun QuokerProgressBar(fraction: Float, modifier: Modifier = Modifier, color: Color = QuokerTheme.colors.onSurface) {
    val shape = RoundedCornerShape(2.dp)
    Box(modifier.fillMaxWidth().height(4.dp).clip(shape).background(QuokerTheme.colors.placeholder)) {
        Box(Modifier.fillMaxHeight().fillMaxWidth(fraction.coerceIn(0f, 1f)).background(color))
    }
}

@Composable
private fun toneColor(tone: RowTextTone, default: Color = QuokerTheme.colors.onSurfaceVariant): Color {
    val c = QuokerTheme.colors
    return when (tone) {
        RowTextTone.Default -> default
        RowTextTone.Muted -> c.onSurfaceVariant
        RowTextTone.Error -> c.error
        RowTextTone.Accent -> c.send
    }
}

@Preview
@Composable
private fun ListsPreview() {
    QuokerTheme {
        Column(
            modifier = Modifier.background(QuokerTheme.colors.background).padding(20.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            ListGroup(header = "Куда отправлять") {
                ListRow("Друзья", subtitle = "Группа", divider = true, trailing = { QuokerSwitch(true, {}) })
                ListRow(
                    "Старый канал",
                    subtitle = "Бот удалён из канала",
                    titleTone = RowTextTone.Muted,
                    subtitleTone = RowTextTone.Error,
                    trailing = { QuokerSwitch(false, null, enabled = false) },
                )
            }
            ListGroup(header = "Приложение") {
                ListRow(
                    "Администрирование",
                    leading = { QuokerIcon(QuokerIcons.Shield, null, tint = QuokerTheme.colors.download, size = 20.dp) },
                    trailing = { ListRowChevron() },
                    onClick = {},
                    minHeight = 56.dp,
                )
            }
            SelectableRow(
                selected = true,
                onClick = {},
                title = "Друзья",
                subtitle = "Группа · последняя",
                leading = { InitialAvatar("Д", tone = AvatarTone.SendSolid) },
            )
            SelectableRow(
                selected = false,
                onClick = {},
                title = "Сохранённое в боте",
                subtitle = "Личные сообщения",
                leading = { InitialAvatar("", icon = QuokerIcons.Bookmark) },
            )
            SegmentedControl(listOf("Пользователь", "Администратор"), 0, {})
            Row(verticalAlignment = Alignment.CenterVertically) {
                QuokerCheckbox(true, {})
                QuokerRadioButton(true, {})
                InitialAvatar("M", tone = AvatarTone.Send, size = 44.dp)
            }
            QuokerProgressBar(0.41f)
        }
    }
}
