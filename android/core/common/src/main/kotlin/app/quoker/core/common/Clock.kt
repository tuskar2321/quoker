package app.quoker.core.common

import java.time.Instant

fun interface Clock {
    fun now(): Instant

    companion object {
        val System: Clock = Clock { Instant.now() }
    }
}
