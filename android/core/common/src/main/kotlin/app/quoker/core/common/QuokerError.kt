package app.quoker.core.common

/**
 * Domain error. [code] is one of the error codes from docs/03-public-api.md; UI text is resolved
 * from `error_<code>` strings, with [message] (from the server) as the fallback.
 */
data class QuokerError(
    val code: String,
    val message: String,
    val retryable: Boolean,
)
