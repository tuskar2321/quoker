package app.quoker.core.common

import org.junit.Assert.assertTrue
import org.junit.Test

class ClockTest {
    @Test
    fun systemClockIsMonotonicEnough() {
        val first = Clock.System.now()
        val second = Clock.System.now()
        assertTrue(!second.isBefore(first))
    }
}
