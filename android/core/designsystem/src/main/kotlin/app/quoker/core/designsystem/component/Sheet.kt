package app.quoker.core.designsystem.component

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.SheetState
import androidx.compose.material3.Text
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import app.quoker.core.designsystem.theme.QuokerTheme

/** The 36×4 sheet handle. */
@Composable
fun SheetHandle(modifier: Modifier = Modifier) {
    val d = QuokerTheme.dimens
    Box(
        modifier
            .padding(top = d.sheetTopPadding)
            .size(d.sheetHandle)
            .background(QuokerTheme.colors.handle, RoundedCornerShape(2.dp)),
    )
}

/**
 * Modal sheet with Quoker styling: surface, radius 28 on top, our handle, side padding 20,
 * bottom padding 28 plus the navigation bar. [spacing] is the gap between children (20 by default,
 * 16 for the error and send sheets).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun QuokerBottomSheet(
    onDismissRequest: () -> Unit,
    modifier: Modifier = Modifier,
    sheetState: SheetState = rememberModalBottomSheetState(skipPartiallyExpanded = true),
    spacing: Dp = QuokerTheme.dimens.sheetGap,
    content: @Composable ColumnScope.() -> Unit,
) {
    val c = QuokerTheme.colors
    ModalBottomSheet(
        onDismissRequest = onDismissRequest,
        modifier = modifier,
        sheetState = sheetState,
        shape = RoundedCornerShape(topStart = QuokerTheme.dimens.sheetRadius, topEnd = QuokerTheme.dimens.sheetRadius),
        containerColor = c.surface,
        contentColor = c.onSurface,
        tonalElevation = 0.dp,
        scrimColor = c.scrim,
        dragHandle = { SheetHandle() },
    ) {
        SheetBody(spacing = spacing, includeHandle = false, content = content)
    }
}

/**
 * Sheet layout without the modal host: the same paddings and spacing. Use it to draw sheet content
 * on a surface yourself (e.g. a custom host in ShareActivity) and in previews.
 */
@Composable
fun SheetBody(
    modifier: Modifier = Modifier,
    spacing: Dp = QuokerTheme.dimens.sheetGap,
    includeHandle: Boolean = true,
    content: @Composable ColumnScope.() -> Unit,
) {
    val d = QuokerTheme.dimens
    Column(
        modifier = modifier
            .fillMaxWidth()
            .then(
                if (includeHandle) {
                    Modifier.background(
                        QuokerTheme.colors.surface,
                        RoundedCornerShape(topStart = d.sheetRadius, topEnd = d.sheetRadius),
                    )
                } else {
                    Modifier
                },
            )
            .navigationBarsPadding()
            .padding(start = d.sheetPadding, end = d.sheetPadding, bottom = d.sheetBottomPadding),
        verticalArrangement = Arrangement.spacedBy(spacing),
    ) {
        if (includeHandle) SheetHandle(Modifier.align(Alignment.CenterHorizontally))
        else Spacer(Modifier.size(0.dp)) // keeps the handle-to-content gap under the modal handle
        content()
    }
}

@Preview
@Composable
private fun SheetBodyPreview() {
    QuokerTheme {
        Box(Modifier.background(QuokerTheme.colors.background)) {
            SheetBody {
                Text("@author", style = QuokerTheme.typography.titleSmall)
                PrimaryButton("Скачать", {}, variant = PrimaryButtonVariant.Download)
            }
        }
    }
}
