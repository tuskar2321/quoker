package app.quoker.core.designsystem.theme

import androidx.compose.material3.ColorScheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Immutable
import androidx.compose.ui.graphics.Color

/** Color tokens from docs/09-design.md. Dark only; two meaningful accents: download and send. */
@Immutable
data class QuokerColors(
    val background: Color,
    val surface: Color,
    val surfaceVariant: Color,
    val placeholder: Color,
    val outline: Color,
    val divider: Color,
    val handle: Color,
    val onSurface: Color,
    val onSurfaceStrong: Color,
    val onSurfaceVariant: Color,
    val download: Color,
    val onDownload: Color,
    val downloadContainer: Color,
    val onDownloadContainer: Color,
    val downloadProgress: Color,
    val onDownloadProgress: Color,
    val send: Color,
    val onSend: Color,
    val sendContainer: Color,
    val onSendContainer: Color,
    /** Status text on [sendContainer] («Отправлено в …»). */
    val onSendContainerStrong: Color,
    /** Secondary text on a selected target row. */
    val onSendContainerVariant: Color,
    val error: Color,
    val errorContainer: Color,
    /** Alert title on [errorContainer] (AdminStatus mockup). */
    val onErrorContainer: Color,
    /** Alert body on [errorContainer]. */
    val onErrorContainerVariant: Color,
    val scrim: Color,
)

val DarkQuokerColors = QuokerColors(
    background = Color(0xFF000000),
    surface = Color(0xFF141414),
    surfaceVariant = Color(0xFF1F1F1F),
    placeholder = Color(0xFF2A2A2A),
    outline = Color(0xFF2E2E2E),
    divider = Color(0xFF1F1F1F),
    handle = Color(0xFF4D4D4D),
    onSurface = Color(0xFFF3F3F5),
    onSurfaceStrong = Color(0xFFC6C8CE),
    onSurfaceVariant = Color(0xFFA9ABB3),
    download = Color(0xFFC5F06B),
    onDownload = Color(0xFF141414),
    downloadContainer = Color(0xFF1F2614),
    onDownloadContainer = Color(0xFFD6F59A),
    downloadProgress = Color(0xFF36451C),
    onDownloadProgress = Color(0xFFE6F8C4),
    send = Color(0xFF6AB3F5),
    onSend = Color(0xFF141414),
    sendContainer = Color(0xFF17242F),
    onSendContainer = Color(0xFF9CCBF8),
    onSendContainerStrong = Color(0xFFCFE6FB),
    onSendContainerVariant = Color(0xFFB8D9F7),
    error = Color(0xFFFF9EA6),
    errorContainer = Color(0xFF2E1A1C),
    onErrorContainer = Color(0xFFFFD0D4),
    onErrorContainerVariant = Color(0xFFE8B7BC),
    scrim = Color(0x99000000),
)

/** Maps the tokens onto Material 3 roles so stock components (dialogs, switches, menus) fit. */
fun QuokerColors.toMaterialColorScheme(): ColorScheme = darkColorScheme(
    primary = onSurface,
    onPrimary = background,
    primaryContainer = surfaceVariant,
    onPrimaryContainer = onSurface,
    inversePrimary = background,
    secondary = send,
    onSecondary = onSend,
    secondaryContainer = sendContainer,
    onSecondaryContainer = onSendContainer,
    tertiary = download,
    onTertiary = onDownload,
    tertiaryContainer = downloadContainer,
    onTertiaryContainer = onDownloadContainer,
    background = background,
    onBackground = onSurface,
    surface = surface,
    onSurface = onSurface,
    surfaceVariant = surfaceVariant,
    onSurfaceVariant = onSurfaceVariant,
    surfaceTint = Color.Transparent,
    inverseSurface = onSurface,
    inverseOnSurface = background,
    error = error,
    onError = background,
    errorContainer = errorContainer,
    onErrorContainer = onErrorContainer,
    outline = outline,
    outlineVariant = divider,
    scrim = scrim,
    surfaceBright = surfaceVariant,
    surfaceDim = background,
    surfaceContainerLowest = background,
    surfaceContainerLow = surface,
    surfaceContainer = surface,
    surfaceContainerHigh = surface,
    surfaceContainerHighest = surfaceVariant,
)
