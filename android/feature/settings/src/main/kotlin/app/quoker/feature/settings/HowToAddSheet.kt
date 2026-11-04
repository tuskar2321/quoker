package app.quoker.feature.settings

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import app.quoker.core.designsystem.component.PrimaryButton
import app.quoker.core.designsystem.component.QuokerBottomSheet
import app.quoker.core.designsystem.component.SectionHeader
import app.quoker.core.designsystem.component.SheetBody
import app.quoker.core.designsystem.theme.QuokerTheme

/** In-app version of the bot instructions from docs/06 (groups: add or /connect; channels: admin + «Обновить»). */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun HowToAddSheet(onDismiss: () -> Unit) {
    QuokerBottomSheet(onDismissRequest = onDismiss) {
        HowToAddContent(onDismiss)
    }
}

@Composable
private fun ColumnScope.HowToAddContent(onDismiss: () -> Unit) {
    Text(
        stringResource(R.string.howto_title),
        style = QuokerTheme.typography.titleMedium,
        color = QuokerTheme.colors.onSurface,
        modifier = Modifier.semantics { heading() },
    )
    StepGroup(
        stringResource(R.string.howto_group_title),
        listOf(
            stringResource(R.string.howto_group_step1),
            stringResource(R.string.howto_group_step2),
            stringResource(R.string.howto_group_step3),
        ),
    )
    StepGroup(
        stringResource(R.string.howto_channel_title),
        listOf(stringResource(R.string.howto_channel_step1), stringResource(R.string.howto_channel_step2)),
    )
    PrimaryButton(stringResource(R.string.howto_done), onDismiss)
}

@Composable
private fun StepGroup(title: String, steps: List<String>) {
    Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
        SectionHeader(title)
        steps.forEachIndexed { index, step ->
            Row(horizontalArrangement = Arrangement.spacedBy(12.dp), verticalAlignment = Alignment.Top) {
                Box(
                    Modifier.size(24.dp).background(QuokerTheme.colors.surfaceVariant, CircleShape),
                    contentAlignment = Alignment.Center,
                ) {
                    Text(
                        "${index + 1}",
                        style = QuokerTheme.typography.labelSmall.copy(fontWeight = FontWeight.W800, fontSize = 12.sp),
                        color = QuokerTheme.colors.onSurface,
                    )
                }
                Text(step, style = QuokerTheme.typography.bodyMedium, color = QuokerTheme.colors.onSurfaceStrong)
            }
        }
    }
}

@Preview(widthDp = 390)
@Composable
private fun HowToAddPreview() {
    QuokerTheme {
        SheetBody { HowToAddContent(onDismiss = {}) }
    }
}
