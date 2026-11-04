package app.quoker.core.designsystem.component

import androidx.annotation.DrawableRes
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.interaction.MutableInteractionSource
import androidx.compose.foundation.interaction.collectIsFocusedAsState
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.text.input.InputTransformation
import androidx.compose.foundation.text.input.KeyboardActionHandler
import androidx.compose.foundation.text.input.OutputTransformation
import androidx.compose.foundation.text.input.TextFieldLineLimits
import androidx.compose.foundation.text.input.TextFieldState
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import app.quoker.core.designsystem.icon.QuokerIcon
import app.quoker.core.designsystem.icon.QuokerIcons
import app.quoker.core.designsystem.theme.QuokerTheme

/**
 * Single-line field: 56 high, radius 16, surface background, `outline` border; focus shows a 1.5
 * [focusedBorderColor] border. [label] is drawn above, [supportingText] below («Формат: XXXX-XXXX»).
 * [trailing] holds actions such as [FieldIconAction] («Вставить»).
 */
@Composable
fun QuokerTextField(
    value: String,
    onValueChange: (String) -> Unit,
    modifier: Modifier = Modifier,
    label: String? = null,
    placeholder: String? = null,
    supportingText: String? = null,
    isError: Boolean = false,
    enabled: Boolean = true,
    textStyle: TextStyle = QuokerTheme.typography.bodyLarge.copy(fontSize = 16.sp, fontWeight = FontWeight.W500),
    focusedBorderColor: Color = QuokerTheme.colors.download,
    keyboardOptions: KeyboardOptions = KeyboardOptions.Default,
    keyboardActions: KeyboardActions = KeyboardActions.Default,
    visualTransformation: VisualTransformation = VisualTransformation.None,
    trailing: (@Composable RowScope.() -> Unit)? = null,
) {
    FieldScaffold(label, supportingText, isError, modifier) {
        FieldBox(
            value = value,
            onValueChange = onValueChange,
            placeholder = placeholder,
            isError = isError,
            enabled = enabled,
            textStyle = textStyle,
            focusedBorderColor = focusedBorderColor,
            keyboardOptions = keyboardOptions,
            keyboardActions = keyboardActions,
            visualTransformation = visualTransformation,
            singleLine = true,
            trailing = trailing,
        )
    }
}

/**
 * [QuokerTextField] over a [TextFieldState]. Use it when the text is rewritten while typing
 * (upper-casing, filtering): [inputTransformation] edits the buffer the IME works on, so fast
 * typing loses nothing, and [outputTransformation] adds display-only characters such as a dash.
 */
@Composable
fun QuokerTextField(
    state: TextFieldState,
    modifier: Modifier = Modifier,
    label: String? = null,
    placeholder: String? = null,
    supportingText: String? = null,
    isError: Boolean = false,
    enabled: Boolean = true,
    textStyle: TextStyle = QuokerTheme.typography.bodyLarge.copy(fontSize = 16.sp, fontWeight = FontWeight.W500),
    focusedBorderColor: Color = QuokerTheme.colors.download,
    keyboardOptions: KeyboardOptions = KeyboardOptions.Default,
    onKeyboardAction: KeyboardActionHandler? = null,
    inputTransformation: InputTransformation? = null,
    outputTransformation: OutputTransformation? = null,
    trailing: (@Composable RowScope.() -> Unit)? = null,
) {
    val c = QuokerTheme.colors
    val interaction = remember { MutableInteractionSource() }
    val focused by interaction.collectIsFocusedAsState()
    FieldScaffold(label, supportingText, isError, modifier) {
        BasicTextField(
            state = state,
            enabled = enabled,
            inputTransformation = inputTransformation,
            outputTransformation = outputTransformation,
            lineLimits = TextFieldLineLimits.SingleLine,
            textStyle = textStyle.copy(color = if (enabled) c.onSurface else c.onSurfaceVariant),
            cursorBrush = SolidColor(c.download),
            keyboardOptions = keyboardOptions,
            onKeyboardAction = onKeyboardAction,
            interactionSource = interaction,
            modifier = Modifier.fillMaxWidth(),
            decorator = { inner ->
                FieldDecoration(
                    isEmpty = state.text.isEmpty(),
                    placeholder = placeholder,
                    isError = isError,
                    focused = focused,
                    textStyle = textStyle,
                    focusedBorderColor = focusedBorderColor,
                    singleLine = true,
                    trailing = trailing,
                    inner = inner,
                )
            },
        )
    }
}

/** Multi-line field («Подпись»): radius 14, black background, 2+ lines. */
@Composable
fun QuokerTextArea(
    value: String,
    onValueChange: (String) -> Unit,
    modifier: Modifier = Modifier,
    label: String? = null,
    placeholder: String? = null,
    supportingText: String? = null,
    isError: Boolean = false,
    enabled: Boolean = true,
    minLines: Int = 2,
    maxLines: Int = 6,
    keyboardOptions: KeyboardOptions = KeyboardOptions.Default,
) {
    FieldScaffold(label, supportingText, isError, modifier) {
        FieldBox(
            value = value,
            onValueChange = onValueChange,
            placeholder = placeholder,
            isError = isError,
            enabled = enabled,
            textStyle = QuokerTheme.typography.bodyMedium,
            focusedBorderColor = QuokerTheme.colors.handle,
            keyboardOptions = keyboardOptions,
            keyboardActions = KeyboardActions.Default,
            visualTransformation = VisualTransformation.None,
            singleLine = false,
            minLines = minLines,
            maxLines = maxLines,
            trailing = null,
        )
    }
}

/** Square 44 action inside a field (surfaceVariant, radius 12, 20dp icon). */
@Composable
fun FieldIconAction(
    @DrawableRes icon: Int,
    contentDescription: String,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    enabled: Boolean = true,
) {
    val c = QuokerTheme.colors
    Box(
        modifier = modifier
            .size(44.dp)
            .clip(RoundedCornerShape(12.dp))
            .background(c.surfaceVariant)
            .clickable(enabled = enabled, role = Role.Button, onClickLabel = contentDescription, onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        QuokerIcon(icon, contentDescription, tint = if (enabled) c.onSurface else c.onSurfaceVariant, size = 20.dp)
    }
}

@Composable
private fun FieldScaffold(
    label: String?,
    supportingText: String?,
    isError: Boolean,
    modifier: Modifier,
    field: @Composable () -> Unit,
) {
    val c = QuokerTheme.colors
    Column(modifier = modifier, verticalArrangement = Arrangement.spacedBy(8.dp)) {
        if (label != null) {
            Text(label, style = QuokerTheme.typography.labelMedium, color = c.onSurfaceVariant)
        }
        field()
        if (supportingText != null) {
            Text(
                supportingText,
                style = QuokerTheme.typography.bodySmall,
                color = if (isError) c.error else c.onSurfaceVariant,
            )
        }
    }
}

@Composable
private fun FieldBox(
    value: String,
    onValueChange: (String) -> Unit,
    placeholder: String?,
    isError: Boolean,
    enabled: Boolean,
    textStyle: TextStyle,
    focusedBorderColor: Color,
    keyboardOptions: KeyboardOptions,
    keyboardActions: KeyboardActions,
    visualTransformation: VisualTransformation,
    singleLine: Boolean,
    trailing: (@Composable RowScope.() -> Unit)?,
    minLines: Int = 1,
    maxLines: Int = 1,
) {
    val c = QuokerTheme.colors
    val interaction = remember { MutableInteractionSource() }
    val focused by interaction.collectIsFocusedAsState()
    BasicTextField(
        value = value,
        onValueChange = onValueChange,
        enabled = enabled,
        singleLine = singleLine,
        minLines = if (singleLine) 1 else minLines,
        maxLines = if (singleLine) 1 else maxLines,
        textStyle = textStyle.copy(color = if (enabled) c.onSurface else c.onSurfaceVariant),
        cursorBrush = SolidColor(c.download),
        keyboardOptions = keyboardOptions,
        keyboardActions = keyboardActions,
        visualTransformation = visualTransformation,
        interactionSource = interaction,
        modifier = Modifier.fillMaxWidth(),
        decorationBox = { inner ->
            FieldDecoration(
                isEmpty = value.isEmpty(),
                placeholder = placeholder,
                isError = isError,
                focused = focused,
                textStyle = textStyle,
                focusedBorderColor = focusedBorderColor,
                singleLine = singleLine,
                trailing = trailing,
                inner = inner,
            )
        },
    )
}

@Composable
private fun FieldDecoration(
    isEmpty: Boolean,
    placeholder: String?,
    isError: Boolean,
    focused: Boolean,
    textStyle: TextStyle,
    focusedBorderColor: Color,
    singleLine: Boolean,
    trailing: (@Composable RowScope.() -> Unit)?,
    inner: @Composable () -> Unit,
) {
    val c = QuokerTheme.colors
    val shape = RoundedCornerShape(if (singleLine) QuokerTheme.dimens.fieldRadius else 14.dp)
    val (borderWidth, borderColor) = when {
        isError -> 1.5.dp to c.error
        focused -> 1.5.dp to focusedBorderColor
        else -> 1.dp to c.outline
    }
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .then(
                if (singleLine) Modifier.height(QuokerTheme.dimens.fieldHeight) else Modifier.defaultMinSize(minHeight = 72.dp),
            )
            .background(if (singleLine) c.surface else c.background, shape)
            .border(borderWidth, borderColor, shape)
            .padding(
                start = if (singleLine) 16.dp else 14.dp,
                end = if (trailing != null) 6.dp else if (singleLine) 16.dp else 14.dp,
                top = if (singleLine) 0.dp else 12.dp,
                bottom = if (singleLine) 0.dp else 12.dp,
            ),
        verticalAlignment = if (singleLine) Alignment.CenterVertically else Alignment.Top,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Box(modifier = Modifier.weight(1f)) {
            if (isEmpty && placeholder != null) {
                Text(placeholder, style = textStyle, color = c.onSurfaceVariant, maxLines = 1)
            }
            inner()
        }
        trailing?.invoke(this)
    }
}

@Preview
@Composable
private fun FieldsPreview() {
    QuokerTheme {
        Column(
            modifier = Modifier.background(QuokerTheme.colors.background).padding(20.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            QuokerTextField(
                value = "",
                onValueChange = {},
                label = "Ссылка на TikTok или Reels",
                placeholder = "https://",
                trailing = { FieldIconAction(QuokerIcons.Paste, "Вставить", {}) },
            )
            QuokerTextField(
                value = "K7Q2-",
                onValueChange = {},
                label = "Код приглашения",
                supportingText = "Формат: XXXX-XXXX",
                textStyle = QuokerTheme.typography.titleMedium.copy(fontWeight = FontWeight.W700, letterSpacing = 3.sp),
            )
            QuokerTextField(value = "ABCD", onValueChange = {}, isError = true, supportingText = "Код не подошёл")
            QuokerTextArea(value = "https://vm.tiktok.com/ZM…", onValueChange = {}, label = "Подпись")
        }
    }
}
