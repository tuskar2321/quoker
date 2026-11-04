package app.quoker.core.network

import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.coroutines.channels.BufferOverflow
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.asSharedFlow

sealed interface SessionEvent {
    /** 401 on a request that carried a session token: the session is gone. */
    data object SignedOut : SessionEvent

    /** 426: the app is below min_supported_version_code. */
    data object UpgradeRequired : SessionEvent

    /** 403 user_blocked. */
    data object Blocked : SessionEvent
}

/** App-wide session signals raised by the API client; not replayed to late collectors. */
@Singleton
class SessionEvents @Inject constructor() {
    private val flow = MutableSharedFlow<SessionEvent>(
        extraBufferCapacity = 16,
        onBufferOverflow = BufferOverflow.DROP_OLDEST,
    )

    val events: SharedFlow<SessionEvent> = flow.asSharedFlow()

    fun emit(event: SessionEvent) {
        flow.tryEmit(event)
    }
}
