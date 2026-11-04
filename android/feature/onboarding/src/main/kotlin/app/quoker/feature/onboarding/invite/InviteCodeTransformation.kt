package app.quoker.feature.onboarding.invite

import androidx.compose.foundation.text.input.InputTransformation
import androidx.compose.foundation.text.input.OutputTransformation
import androidx.compose.foundation.text.input.TextFieldBuffer
import androidx.compose.foundation.text.input.delete
import androidx.compose.foundation.text.input.insert
import app.quoker.core.common.format.InviteCode

/**
 * Keeps the field's buffer equal to [InviteCode.normalize] of what was typed: upper-cases letters,
 * drops other characters (including a typed or pasted dash) and cuts to 8. Edits are made in place,
 * so the cursor stays where the user types.
 */
internal object InviteCodeInputTransformation : InputTransformation {
    override fun TextFieldBuffer.transformInput() {
        for (i in length - 1 downTo 0) {
            val ch = charAt(i)
            val upper = ch.uppercaseChar()
            when {
                upper !in 'A'..'Z' && upper !in '0'..'9' -> delete(i, i + 1)
                upper != ch -> replace(i, i + 1, upper.toString())
            }
        }
        if (length > InviteCode.LENGTH) delete(InviteCode.LENGTH, length)
    }
}

/** Shows the dash after the first group («K7Q2-X9PL»); the buffer keeps only the characters. */
internal object InviteCodeOutputTransformation : OutputTransformation {
    private const val GROUP = 4

    override fun TextFieldBuffer.transformOutput() {
        if (length >= GROUP) insert(GROUP, "-")
    }
}
