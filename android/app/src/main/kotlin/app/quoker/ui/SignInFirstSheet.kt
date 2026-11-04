package app.quoker.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import app.quoker.R
import app.quoker.core.designsystem.component.IconContainer
import app.quoker.core.designsystem.component.IconContainerTone
import app.quoker.core.designsystem.component.PrimaryButton
import app.quoker.core.designsystem.component.QuokerBottomSheet
import app.quoker.core.designsystem.component.SecondaryButton
import app.quoker.core.designsystem.icon.QuokerIcons
import app.quoker.core.designsystem.theme.QuokerTheme
import app.quoker.core.designsystem.R as DesignR

/** ShareActivity without a session: «Сначала войдите в Quoker». */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun SignInFirstSheet(onOpenApp: () -> Unit, onDismiss: () -> Unit) {
    val c = QuokerTheme.colors
    val d = QuokerTheme.dimens
    QuokerBottomSheet(onDismissRequest = onDismiss) {
        Column(
            modifier = Modifier.fillMaxWidth(),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(d.buttonGap),
        ) {
            IconContainer(QuokerIcons.User, tone = IconContainerTone.Neutral)
            Text(
                stringResource(R.string.share_sign_in_title),
                style = QuokerTheme.typography.titleLarge,
                color = c.onSurface,
                textAlign = TextAlign.Center,
            )
            Text(
                stringResource(R.string.share_sign_in_text),
                style = QuokerTheme.typography.bodyParagraph,
                color = c.onSurfaceVariant,
                textAlign = TextAlign.Center,
            )
        }
        Column(Modifier.fillMaxWidth(), verticalArrangement = Arrangement.spacedBy(d.buttonGap)) {
            PrimaryButton(text = stringResource(R.string.share_open_app), onClick = onOpenApp)
            SecondaryButton(text = stringResource(DesignR.string.action_close), onClick = onDismiss)
        }
    }
}
