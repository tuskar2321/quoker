package app.quoker.feature.onboarding

import androidx.compose.foundation.text.input.TextFieldState
import androidx.compose.foundation.text.input.delete
import androidx.compose.foundation.text.input.insert
import androidx.compose.ui.text.TextRange
import app.quoker.feature.onboarding.invite.InviteCodeInputTransformation
import org.junit.Assert.assertEquals
import org.junit.Test

class InviteCodeTransformationTest {
    /** Applies one user edit to [initial] (cursor at [cursor]) and the transformation, like the field does. */
    private fun edit(initial: String, cursor: Int = initial.length, change: androidx.compose.foundation.text.input.TextFieldBuffer.() -> Unit): Pair<String, Int> {
        val state = TextFieldState(initial, TextRange(cursor))
        state.edit {
            change()
            with(InviteCodeInputTransformation) { transformInput() }
        }
        return state.text.toString() to state.selection.start
    }

    @Test
    fun typingFormatsAndKeepsCursorAfterTheDash() {
        data class Case(val name: String, val initial: String, val cursor: Int, val typed: String, val text: String, val at: Int)
        val cases = listOf(
            Case("first chars", "", 0, "k7q", "K7Q", 3),
            Case("fourth char adds the dash, cursor after it", "K7Q", 3, "2", "K7Q2-", 5),
            Case("fifth char", "K7Q2-", 5, "x", "K7Q2-X", 6),
            Case("typed before the dash", "K7Q2-", 4, "x", "K7Q2-X", 6),
            Case("paste with dash and spaces", "", 0, " k7q2-x9pl ", "K7Q2-X9PL", 9),
            Case("cut to 8", "K7Q2-X9PL", 9, "Z", "K7Q2-X9PL", 9),
            Case("typed in the middle", "K7Q2-X9", 2, "a", "K7AQ-2X9", 3),
            Case("non-latin dropped", "", 0, "кq7", "Q7", 2),
        )
        for (case in cases) {
            val (text, at) = edit(case.initial, case.cursor) { insert(case.cursor, case.typed) }
            assertEquals(case.name, case.text, text)
            assertEquals(case.name, case.at, at)
        }
    }

    @Test
    fun backspace() {
        data class Case(val name: String, val initial: String, val cursor: Int, val text: String, val at: Int)
        val cases = listOf(
            Case("right after the dash removes the char before it", "K7Q2-", 5, "K7Q", 3),
            Case("after the dash in a longer code", "K7Q2-X9", 5, "K7QX-9", 3),
            Case("last char", "K7Q2-X9", 7, "K7Q2-X", 6),
            Case("first group", "K7Q", 3, "K7", 2),
        )
        for (case in cases) {
            val (text, at) = edit(case.initial, case.cursor) { delete(case.cursor - 1, case.cursor) }
            assertEquals(case.name, case.text, text)
            assertEquals(case.name, case.at, at)
        }
    }
}
