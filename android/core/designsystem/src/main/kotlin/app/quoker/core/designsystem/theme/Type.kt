package app.quoker.core.designsystem.theme

import androidx.compose.material3.Typography
import androidx.compose.runtime.Immutable
import androidx.compose.ui.text.ExperimentalTextApi
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.Font
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontVariation
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.sp
import app.quoker.core.designsystem.R

@OptIn(ExperimentalTextApi::class)
private fun manrope(weight: Int) = Font(
    resId = R.font.manrope,
    weight = FontWeight(weight),
    variationSettings = FontVariation.Settings(FontVariation.weight(weight)),
)

/** Manrope variable font (res/font/manrope.ttf, OFL: res/raw/manrope_ofl.txt), weights 400–800. */
val Manrope = FontFamily(manrope(400), manrope(500), manrope(600), manrope(700), manrope(800))

/**
 * Text styles from docs/09-design.md. [overline] must be shown upper-cased: use
 * `SectionHeader`, which does it, rather than applying the style to raw text.
 */
@Immutable
data class QuokerTypography(
    val displaySmall: TextStyle,
    val titleLarge: TextStyle,
    val titleMedium: TextStyle,
    val brand: TextStyle,
    val titleSmall: TextStyle,
    val bodyLarge: TextStyle,
    /** 15/500, line height 1.5: paragraphs (onboarding, error text). */
    val bodyParagraph: TextStyle,
    val bodyMedium: TextStyle,
    /** 13/500: hints, meta lines («14 МБ · без водяного знака»). */
    val bodySmall: TextStyle,
    val labelLarge: TextStyle,
    val labelMedium: TextStyle,
    val labelSmall: TextStyle,
    val overline: TextStyle,
)

private fun style(size: Int, weight: Int, lineHeight: Float = 1.3f, tracking: Float = 0f) = TextStyle(
    fontFamily = Manrope,
    fontSize = size.sp,
    fontWeight = FontWeight(weight),
    lineHeight = (size * lineHeight).sp,
    letterSpacing = tracking.sp,
)

val DefaultQuokerTypography = QuokerTypography(
    displaySmall = style(28, 800, lineHeight = 1.2f),
    titleLarge = style(22, 800, lineHeight = 1.25f),
    titleMedium = style(20, 800, lineHeight = 1.25f),
    brand = style(24, 800, lineHeight = 1.2f, tracking = -0.4f),
    titleSmall = style(17, 700),
    bodyLarge = style(15, 600),
    bodyParagraph = style(15, 500, lineHeight = 1.5f),
    bodyMedium = style(14, 500, lineHeight = 1.45f),
    bodySmall = style(13, 500, lineHeight = 1.45f),
    labelLarge = style(16, 800, lineHeight = 1.25f),
    labelMedium = style(13, 600),
    labelSmall = style(12, 600),
    overline = style(13, 700, tracking = 0.6f),
)

fun QuokerTypography.toMaterialTypography(): Typography = Typography(
    displaySmall = displaySmall,
    headlineSmall = titleLarge,
    titleLarge = titleLarge,
    titleMedium = titleMedium,
    titleSmall = titleSmall,
    bodyLarge = bodyLarge,
    bodyMedium = bodyMedium,
    bodySmall = bodySmall,
    labelLarge = labelLarge,
    labelMedium = labelMedium,
    labelSmall = labelSmall,
)
