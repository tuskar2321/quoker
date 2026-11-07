package app.quoker.feature.onboarding.invite

import androidx.compose.foundation.text.input.InputTransformation
import androidx.compose.foundation.text.input.TextFieldBuffer
import androidx.compose.foundation.text.input.delete
import androidx.compose.foundation.text.input.insert
import androidx.compose.ui.text.TextRange
import app.quoker.core.common.format.InviteCode

/**
 * Keeps the field formatted as «K7Q2-X9PL»: letters upper-cased, other characters dropped, at most
 * 8, and the dash after the first group as soon as it is complete. The dash lives in the buffer
 * (not in an OutputTransformation) so the cursor can stand after it: a display-only insertion at
 * the cursor puts the cursor before it.
 */
internal object InviteCodeInputTransformation : InputTransformation {
    private const val GROUP = 4

    override fun TextFieldBuffer.transformInput() {
        val current = asCharSequence().toString()
        val original = originalText.toString()
        var cursorChars = alnumCount(current, selection.start)
        // Backspace right after the dash removed only the dash: remove the character before it too,
        // otherwise formatting puts the dash back and the cursor gets stuck.
        val dashDeleted = selection.collapsed &&
            original.getOrNull(GROUP) == '-' && current == original.removeRange(GROUP, GROUP + 1)

        // Small in-place edits only: rewriting the whole text on every key makes the IME drop
        // characters typed in quick succession.
        for (i in length - 1 downTo 0) {
            val ch = charAt(i)
            val upper = ch.uppercaseChar()
            when {
                upper !in 'A'..'Z' && upper !in '0'..'9' -> delete(i, i + 1)
                upper != ch -> replace(i, i + 1, upper.toString())
            }
        }
        if (dashDeleted) {
            delete(GROUP - 1, GROUP)
            cursorChars = GROUP - 1
        }
        if (length > InviteCode.LENGTH) delete(InviteCode.LENGTH, length)
        val chars = length
        if (chars >= GROUP) insert(GROUP, "-")
        val cursor = cursorChars.coerceAtMost(chars)
        selection = TextRange(if (cursor >= GROUP && chars >= GROUP) cursor + 1 else cursor)
    }

    private fun alnumCount(text: String, end: Int): Int =
        text.subSequence(0, end.coerceIn(0, text.length)).count { it.uppercaseChar() in 'A'..'Z' || it in '0'..'9' }

    /** «K7Q2-X9PL»; the dash appears once the first group is complete («K7Q2-»). */
    fun format(chars: String): String =
        if (chars.length >= GROUP) chars.substring(0, GROUP) + "-" + chars.substring(GROUP) else chars
}
