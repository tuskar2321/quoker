package app.quoker.core.network

import app.quoker.core.common.QuokerError
import app.quoker.core.network.model.ErrorBodyDto
import java.io.IOException
import javax.inject.Inject
import javax.inject.Singleton
import kotlinx.serialization.json.Json
import okhttp3.Interceptor
import okhttp3.Request
import okhttp3.Response

/**
 * Turns every non-2xx response into [QuokerException] (parsed from `{error:{code,message,retryable}}`,
 * or derived from the HTTP status when the body is not ours) and every transport failure into
 * [ErrorCodes.NETWORK]. Raises [SessionEvents] on the way. Must run after [AuthHeaderInterceptor].
 */
@Singleton
class ErrorMappingInterceptor @Inject constructor(
    private val json: Json,
    private val events: SessionEvents,
) : Interceptor {
    override fun intercept(chain: Interceptor.Chain): Response {
        val request = chain.request()
        val response = try {
            chain.proceed(request)
        } catch (e: QuokerException) {
            throw e
        } catch (e: IOException) {
            throw networkException(e)
        }
        if (response.isSuccessful) return response

        val error = response.use { parseError(it) }
        signalSession(events, request, response.code, error.code)
        throw QuokerException(
            error = error,
            httpStatus = response.code,
            retryAfterSeconds = response.header("Retry-After")?.trim()?.toLongOrNull(),
        )
    }

    private fun parseError(response: Response): QuokerError {
        val body = try {
            response.body.string()
        } catch (_: IOException) {
            ""
        }
        val dto = runCatching { json.decodeFromString<ErrorBodyDto>(body).error }.getOrNull()
        return if (dto != null && dto.code.isNotEmpty()) {
            QuokerError(dto.code, dto.message, dto.retryable)
        } else {
            fallbackError(response.code)
        }
    }
}

/** Raises session events from a response without consuming it; for clients that do not map errors. */
@Singleton
class SessionSignalInterceptor @Inject constructor(
    private val json: Json,
    private val events: SessionEvents,
) : Interceptor {
    override fun intercept(chain: Interceptor.Chain): Response {
        val request = chain.request()
        val response = chain.proceed(request)
        if (response.code == 401 || response.code == 403 || response.code == 426) {
            val code = runCatching {
                json.decodeFromString<ErrorBodyDto>(response.peekBody(MAX_ERROR_PEEK).string()).error.code
            }.getOrNull() ?: fallbackError(response.code).code
            signalSession(events, request, response.code, code)
        }
        return response
    }

    private companion object {
        const val MAX_ERROR_PEEK = 16L * 1024
    }
}

internal fun signalSession(events: SessionEvents, request: Request, status: Int, code: String) {
    when {
        status == 401 && request.header(AuthHeaderInterceptor.HEADER_AUTHORIZATION) != null ->
            events.emit(SessionEvent.SignedOut)
        status == 426 || code == ErrorCodes.UPGRADE_REQUIRED -> events.emit(SessionEvent.UpgradeRequired)
        status == 403 && code == ErrorCodes.USER_BLOCKED -> events.emit(SessionEvent.Blocked)
    }
}

/** Error for responses whose body is not the core's error JSON (proxy pages, empty bodies). */
internal fun fallbackError(status: Int): QuokerError {
    val code = when (status) {
        400 -> ErrorCodes.INVALID_REQUEST
        401 -> ErrorCodes.UNAUTHORIZED
        403 -> ErrorCodes.FORBIDDEN
        404 -> ErrorCodes.NOT_FOUND
        426 -> ErrorCodes.UPGRADE_REQUIRED
        429 -> ErrorCodes.RATE_LIMITED
        502, 504 -> ErrorCodes.UPSTREAM_ERROR
        503 -> ErrorCodes.WORKER_UNAVAILABLE
        else -> ErrorCodes.INTERNAL
    }
    val retryable = status == 429 || status >= 500
    return QuokerError(code, "HTTP $status", retryable)
}
