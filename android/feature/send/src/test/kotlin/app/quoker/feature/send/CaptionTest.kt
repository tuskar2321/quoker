package app.quoker.feature.send

import org.junit.Assert.assertEquals
import org.junit.Test

class CaptionTest {
    @Test
    fun limits() {
        val emoji = "😀" // two UTF-16 units
        val cases = listOf(
            Triple("short", 1024, "short"),
            Triple("a".repeat(1024), 1024, "a".repeat(1024)),
            Triple("a".repeat(1025), 1024, "a".repeat(1024)),
            Triple(emoji.repeat(512), 1024, emoji.repeat(512)),
            // An emoji straddling the limit is dropped whole rather than split.
            Triple("a" + emoji.repeat(512), 1024, "a" + emoji.repeat(511)),
            Triple("ab$emoji", 3, "ab"),
        )
        for ((input, max, expected) in cases) {
            assertEquals(expected, limitCaption(input, max))
        }
    }
}
