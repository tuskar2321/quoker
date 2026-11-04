package app.quoker.feature.onboarding

import androidx.compose.foundation.text.input.TextFieldState
import app.quoker.feature.onboarding.invite.InviteCodeInputTransformation
import app.quoker.feature.onboarding.invite.InviteCodeOutputTransformation
import org.junit.Assert.assertEquals
import org.junit.Test

class InviteCodeTransformationTest {
    @Test
    fun inputIsNormalized() {
        val cases = listOf(
            "" to "",
            "k7q2" to "K7Q2",
            "K7Q2-X9PL" to "K7Q2X9PL",
            " k7 q2 x9 pl " to "K7Q2X9PL",
            "K7Q2X9PLZZ" to "K7Q2X9PL",
            "кq7" to "Q7",
        )
        for ((typed, expected) in cases) {
            val state = TextFieldState()
            state.edit {
                append(typed)
                with(InviteCodeInputTransformation) { transformInput() }
            }
            assertEquals(typed, expected, state.text.toString())
        }
    }

    @Test
    fun outputInsertsDashAfterFirstGroup() {
        val cases = listOf("" to "", "K7Q" to "K7Q", "K7Q2" to "K7Q2-", "K7Q2X" to "K7Q2-X", "K7Q2X9PL" to "K7Q2-X9PL")
        for ((raw, shown) in cases) {
            val state = TextFieldState(raw)
            var result = ""
            state.edit {
                with(InviteCodeOutputTransformation) { transformOutput() }
                result = toString()
                revertAllChanges()
            }
            assertEquals(raw, shown, result)
        }
    }
}
