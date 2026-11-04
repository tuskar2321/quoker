package app.quoker.core.network

import app.quoker.core.common.QuokerError
import java.io.IOException
import kotlinx.coroutines.CancellationException
import kotlinx.serialization.SerializationException

/**
 * The only exception the network and data layers throw for API failures.
 *
 * It extends [IOException] because OkHttp delivers only IOExceptions thrown by interceptors to
 * the caller; anything else would crash its dispatcher thread.
 */
class QuokerException(
    val error: QuokerError,
    /** HTTP status, or null for a failure without a response ([ErrorCodes.NETWORK]). */
    val httpStatus: Int? = null,
    /** `Retry-After` in seconds when the core sent it (429). */
    val retryAfterSeconds: Long? = null,
    cause: Throwable? = null,
) : IOException(error.message, cause) {
    val code: String get() = error.code
}

/** Error codes of docs/03-public-api.md plus the client-only [NETWORK]. */
object ErrorCodes {
    const val UNAUTHORIZED = "unauthorized"
    const val FORBIDDEN = "forbidden"
    const val USER_BLOCKED = "user_blocked"
    const val UPGRADE_REQUIRED = "upgrade_required"
    const val INVALID_REQUEST = "invalid_request"
    const val INVALID_INVITE = "invalid_invite"
    const val TELEGRAM_NOT_LINKED = "telegram_not_linked"
    const val LINK_TOKEN_EXPIRED = "link_token_expired"
    const val UNSUPPORTED_URL = "unsupported_url"
    const val UNSUPPORTED_CONTENT = "unsupported_content"
    const val PRIVATE_VIDEO = "private_video"
    const val NOT_FOUND = "not_found"
    const val FILE_NOT_READY = "file_not_ready"
    const val TOO_LARGE = "too_large"
    const val TARGET_INACTIVE = "target_inactive"
    const val TARGET_FORBIDDEN = "target_forbidden"
    const val RATE_LIMITED = "rate_limited"
    const val WORKER_UNAVAILABLE = "worker_unavailable"
    const val UPSTREAM_ERROR = "upstream_error"
    const val INTERNAL = "internal"

    /**
     * Client-only: no HTTP response at all (no connectivity, DNS, TLS, timeout, connection
     * reset). Always retryable; UI text comes from `error_network`.
     */
    const val NETWORK = "network"
}

internal fun networkException(cause: IOException): QuokerException = QuokerException(
    error = QuokerError(ErrorCodes.NETWORK, cause.message ?: "network error", retryable = true),
    cause = cause,
)

/**
 * Runs an API call and guarantees that any failure surfaces as [QuokerException]: transport
 * errors while reading a body become [ErrorCodes.NETWORK], undecodable bodies
 * [ErrorCodes.INTERNAL]. Cancellation passes through.
 */
suspend fun <T> quokerCall(block: suspend () -> T): T = try {
    block()
} catch (e: CancellationException) {
    throw e
} catch (e: QuokerException) {
    throw e
} catch (e: IOException) {
    throw networkException(e)
} catch (e: SerializationException) {
    throw QuokerException(
        error = QuokerError(ErrorCodes.INTERNAL, e.message ?: "bad response", retryable = false),
        cause = e,
    )
}
