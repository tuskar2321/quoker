package app.quoker.core.data.repository

import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.update

/** A send started from the "Куда отправить" sheet, as the "Видео" sheet shows it. */
data class TrackedSend(
    val sendId: Long,
    val videoId: String,
    val targetId: Long,
    val targetTitle: String,
)

/**
 * Hands the latest send of a video from feature:send to feature:video without the two
 * depending on each other. The video sheet polls the send itself while it is open
 * (docs/08: polling stops when the sheet closes; the result stays in the history).
 */
@Singleton
class SendTracker @Inject constructor() {
    private val sends = MutableStateFlow<Map<String, TrackedSend>>(emptyMap())

    /** Records [send] as the latest send of its video. */
    fun register(send: TrackedSend) {
        sends.update { it + (send.videoId to send) }
    }

    /** The latest send of [videoId] started in this process, or null. */
    fun observe(videoId: String): Flow<TrackedSend?> =
        sends.map { it[videoId] }.distinctUntilChanged()

    /** Forgets the send of [videoId], e.g. after the user dismissed its result. */
    fun clear(videoId: String) {
        sends.update { it - videoId }
    }
}
