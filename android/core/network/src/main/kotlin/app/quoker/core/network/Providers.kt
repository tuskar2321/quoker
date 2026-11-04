package app.quoker.core.network

/**
 * Supplies the current session token to the HTTP layer; implemented by core:data. Called on
 * OkHttp threads, so it must be cheap and must not require the main thread.
 */
fun interface SessionTokenProvider {
    fun sessionToken(): String?
}

/** Supplies the per-install device UUID for `X-Device-Id`; implemented by core:data. */
fun interface DeviceIdProvider {
    fun deviceId(): String
}
