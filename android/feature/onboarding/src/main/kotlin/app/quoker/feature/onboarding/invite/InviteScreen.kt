package app.quoker.feature.onboarding.invite

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.text.input.rememberTextFieldState
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.snapshotFlow
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.tooling.preview.Preview
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.hilt.lifecycle.viewmodel.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import app.quoker.core.common.QuokerError
import app.quoker.core.designsystem.component.PrimaryButton
import app.quoker.core.designsystem.component.QuokerTextField
import app.quoker.core.designsystem.icon.QuokerLogo
import app.quoker.core.designsystem.text.userMessage
import app.quoker.core.designsystem.theme.QuokerTheme
import app.quoker.feature.onboarding.R

/** Stateful invite screen; [onSignedIn] fires once the session is stored. */
@Composable
fun InviteScreen(
    onSignedIn: (telegramLinked: Boolean) -> Unit,
    viewModel: InviteViewModel = hiltViewModel(),
) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    val signedIn by rememberUpdatedState(onSignedIn)
    LaunchedEffect(viewModel) {
        viewModel.events.collect { event ->
            when (event) {
                is InviteEvent.SignedIn -> signedIn(event.telegramLinked)
            }
        }
    }
    InviteContent(state = state, onCodeChange = viewModel::onCodeChange, onSubmit = viewModel::onSubmit)
}

@Composable
fun InviteContent(
    state: InviteUiState,
    onCodeChange: (String) -> Unit,
    onSubmit: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val c = QuokerTheme.colors
    val t = QuokerTheme.typography
    Box(modifier.fillMaxSize().background(c.background)) {
        Column(
            modifier = Modifier
                .fillMaxSize()
                .safeDrawingPadding()
                .padding(start = QuokerTheme.dimens.onboardingGutter, end = QuokerTheme.dimens.onboardingGutter, top = 72.dp, bottom = 32.dp),
            verticalArrangement = Arrangement.spacedBy(20.dp),
        ) {
            Box(
                Modifier.size(64.dp).background(c.surface, RoundedCornerShape(20.dp)),
                contentAlignment = Alignment.Center,
            ) {
                QuokerLogo(size = 48.dp)
            }
            Text(
                stringResource(R.string.invite_title),
                style = t.displaySmall,
                color = c.onSurface,
                modifier = Modifier.padding(top = 8.dp).semantics { heading() },
            )
            Text(stringResource(R.string.invite_text), style = t.bodyParagraph, color = c.onSurfaceStrong)
            // The field owns its text: binding it to the asynchronously collected UiState and
            // rewriting it in onValueChange dropped characters typed faster than a frame. The
            // ViewModel gets the text with the dash and normalizes it.
            val code = rememberTextFieldState(InviteCodeInputTransformation.format(state.code))
            LaunchedEffect(code) {
                snapshotFlow { code.text.toString() }.collect(onCodeChange)
            }
            QuokerTextField(
                state = code,
                modifier = Modifier.padding(top = 12.dp),
                label = stringResource(R.string.invite_code_label),
                supportingText = state.error?.userMessage() ?: stringResource(R.string.invite_code_hint),
                isError = state.error != null,
                enabled = !state.submitting,
                textStyle = t.titleMedium.copy(fontWeight = FontWeight.W700, letterSpacing = 3.sp),
                inputTransformation = InviteCodeInputTransformation,
                keyboardOptions = KeyboardOptions(
                    capitalization = KeyboardCapitalization.Characters,
                    autoCorrectEnabled = false,
                    keyboardType = KeyboardType.Ascii,
                    imeAction = ImeAction.Done,
                ),
                onKeyboardAction = { onSubmit() },
            )
            Spacer(Modifier.weight(1f))
            PrimaryButton(
                text = stringResource(R.string.invite_continue),
                onClick = onSubmit,
                enabled = buttonEnabled(state),
                loading = state.submitting,
            )
        }
    }
}

// The button stays filled while submitting (spinner) and greys out only for an incomplete code.
private fun buttonEnabled(state: InviteUiState): Boolean = state.canSubmit || state.submitting

@Preview(widthDp = 390, heightDp = 844)
@Composable
private fun InviteEmptyPreview() {
    QuokerTheme { InviteContent(InviteUiState(), {}, {}) }
}

@Preview(widthDp = 390, heightDp = 844)
@Composable
private fun InviteTypingPreview() {
    QuokerTheme { InviteContent(InviteUiState(code = "K7Q2"), {}, {}) }
}

@Preview(widthDp = 390, heightDp = 844)
@Composable
private fun InviteSubmittingPreview() {
    QuokerTheme { InviteContent(InviteUiState(code = "K7Q2X9PL", submitting = true), {}, {}) }
}

@Preview(widthDp = 390, heightDp = 844)
@Composable
private fun InviteErrorPreview() {
    QuokerTheme {
        InviteContent(InviteUiState(code = "K7Q2X9PL", error = QuokerError("invalid_invite", "", false)), {}, {})
    }
}
