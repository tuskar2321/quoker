package app.quoker.ui

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.tooling.preview.Preview
import app.quoker.R
import app.quoker.core.designsystem.component.IconContainer
import app.quoker.core.designsystem.component.IconContainerTone
import app.quoker.core.designsystem.component.SecondaryButton
import app.quoker.core.designsystem.icon.QuokerIcons
import app.quoker.core.designsystem.theme.QuokerTheme

/** «Доступ закрыт» (403 user_blocked): nothing works until the user signs out. */
@Composable
internal fun BlockedScreen(onSignOut: () -> Unit, modifier: Modifier = Modifier) {
    BackHandler(enabled = true) {}
    val c = QuokerTheme.colors
    val d = QuokerTheme.dimens
    var signingOut by remember { mutableStateOf(false) }
    Column(
        modifier = modifier
            .fillMaxSize()
            .background(c.background)
            .safeDrawingPadding()
            .padding(horizontal = d.onboardingGutter, vertical = d.sectionGap),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Spacer(Modifier.weight(1f))
        IconContainer(QuokerIcons.Shield, tone = IconContainerTone.Error)
        Spacer(Modifier.height(d.sheetGap))
        Text(
            stringResource(R.string.blocked_title),
            style = QuokerTheme.typography.titleLarge,
            color = c.onSurface,
            textAlign = TextAlign.Center,
        )
        Spacer(Modifier.height(d.buttonGap))
        Text(
            stringResource(R.string.blocked_text),
            style = QuokerTheme.typography.bodyParagraph,
            color = c.onSurfaceVariant,
            textAlign = TextAlign.Center,
        )
        Spacer(Modifier.weight(1f))
        SecondaryButton(
            text = stringResource(R.string.blocked_sign_out),
            onClick = {
                signingOut = true
                onSignOut()
            },
            loading = signingOut,
            enabled = !signingOut,
        )
    }
}

@Preview(widthDp = 390, heightDp = 844)
@Composable
private fun BlockedScreenPreview() {
    QuokerTheme { BlockedScreen(onSignOut = {}) }
}
