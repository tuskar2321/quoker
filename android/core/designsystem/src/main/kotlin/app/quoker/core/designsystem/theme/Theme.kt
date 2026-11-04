package app.quoker.core.designsystem.theme

import androidx.compose.foundation.text.selection.LocalTextSelectionColors
import androidx.compose.foundation.text.selection.TextSelectionColors
import androidx.compose.material3.LocalContentColor
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Shapes
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.ReadOnlyComposable
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.unit.dp

val LocalQuokerColors = staticCompositionLocalOf { DarkQuokerColors }
val LocalQuokerTypography = staticCompositionLocalOf { DefaultQuokerTypography }
val LocalQuokerDimens = staticCompositionLocalOf { QuokerDimens() }

/** The only theme of the app (dark, AMOLED black). Also configures MaterialTheme for M3 components. */
@Composable
fun QuokerTheme(content: @Composable () -> Unit) {
    val colors = DarkQuokerColors
    val typography = DefaultQuokerTypography
    CompositionLocalProvider(
        LocalQuokerColors provides colors,
        LocalQuokerTypography provides typography,
        LocalQuokerDimens provides QuokerDimens(),
    ) {
        MaterialTheme(
            colorScheme = colors.toMaterialColorScheme(),
            typography = typography.toMaterialTypography(),
            shapes = Shapes(
                extraSmall = RoundedCornerShape(8.dp),
                small = RoundedCornerShape(12.dp),
                medium = RoundedCornerShape(16.dp),
                large = RoundedCornerShape(20.dp),
                extraLarge = RoundedCornerShape(28.dp),
            ),
        ) {
            CompositionLocalProvider(
                LocalContentColor provides colors.onSurface,
                LocalTextSelectionColors provides TextSelectionColors(
                    handleColor = colors.download,
                    backgroundColor = colors.download.copy(alpha = 0.3f),
                ),
                content = content,
            )
        }
    }
}

object QuokerTheme {
    val colors: QuokerColors
        @Composable @ReadOnlyComposable get() = LocalQuokerColors.current
    val typography: QuokerTypography
        @Composable @ReadOnlyComposable get() = LocalQuokerTypography.current
    val dimens: QuokerDimens
        @Composable @ReadOnlyComposable get() = LocalQuokerDimens.current
}
