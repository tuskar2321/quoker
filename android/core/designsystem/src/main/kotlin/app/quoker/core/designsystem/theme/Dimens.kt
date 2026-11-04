package app.quoker.core.designsystem.theme

import androidx.compose.runtime.Immutable
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.DpSize
import androidx.compose.ui.unit.dp

/** Sizes from docs/09-design.md and the mockups. */
@Immutable
data class QuokerDimens(
    val screenGutter: Dp = 20.dp,
    val onboardingGutter: Dp = 24.dp,
    val minTouchTarget: Dp = 44.dp,

    val buttonHeight: Dp = 56.dp,
    val buttonRadius: Dp = 16.dp,
    val buttonGap: Dp = 12.dp,
    val fieldHeight: Dp = 56.dp,
    val fieldRadius: Dp = 16.dp,

    val sheetRadius: Dp = 28.dp,
    val sheetPadding: Dp = 20.dp,
    val sheetTopPadding: Dp = 10.dp,
    val sheetBottomPadding: Dp = 28.dp,
    val sheetHandle: DpSize = DpSize(36.dp, 4.dp),
    val sheetGap: Dp = 20.dp,

    val cardRadius: Dp = 16.dp,
    val cardPadding: Dp = 16.dp,
    val cardPaddingVertical: Dp = 14.dp,
    val listRowMinHeight: Dp = 56.dp,
    val listRowTallMinHeight: Dp = 60.dp,
    val sectionGap: Dp = 24.dp,

    val badgeRadius: Dp = 8.dp,
    val badgePaddingVertical: Dp = 3.dp,
    val badgePaddingHorizontal: Dp = 8.dp,

    val coverSheet: DpSize = DpSize(112.dp, 198.dp),
    val coverSheetRadius: Dp = 14.dp,
    val coverHistory: DpSize = DpSize(64.dp, 88.dp),
    val coverHistoryRadius: Dp = 12.dp,
    val coverRecent: DpSize = DpSize(48.dp, 64.dp),
    val coverRecentRadius: Dp = 10.dp,

    val icon: Dp = 24.dp,
    val iconSmall: Dp = 20.dp,
    val iconButton: Dp = 22.dp,
    val avatar: Dp = 40.dp,
    val avatarLarge: Dp = 44.dp,
    val logoTopBar: Dp = 32.dp,
)
