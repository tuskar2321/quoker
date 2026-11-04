package app.quoker.core.data.repository

import app.quoker.core.network.QuokerException
import app.quoker.core.network.quokerCall
import kotlin.time.Duration
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.flow

/** Consecutive retryable failures (network, 5xx) a poll tolerates before failing the flow. */
internal const val MAX_TRANSIENT_POLL_FAILURES = 3

/**
 * Emits [fetch] results every [interval] until [isTerminal] (the terminal value is emitted, then
 * the flow completes). Collect it only while the screen is visible: cancelling stops the polling.
 */
internal fun <T> poll(interval: Duration, fetch: suspend () -> T, isTerminal: (T) -> Boolean): Flow<T> = flow {
    var failures = 0
    while (true) {
        val value = try {
            quokerCall { fetch() }
        } catch (e: QuokerException) {
            if (!e.error.retryable || ++failures >= MAX_TRANSIENT_POLL_FAILURES) throw e
            delay(interval)
            continue
        }
        failures = 0
        emit(value)
        if (isTerminal(value)) return@flow
        delay(interval)
    }
}.distinctUntilChanged()
