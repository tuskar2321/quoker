package app.quoker.core.common.link

/** Supported video platform; [id] is the video_id prefix used by the API. */
enum class Platform(val id: String) {
    TikTok("tiktok"),
    Instagram("instagram"),
}

/**
 * A recognized, normalized link (the same result as the core's internal/shared/urls).
 *
 * [key] is the normalized form used for dedup; [url] is the canonical address the worker fetches.
 * [nativeId] is null for short links, whose id is known only after the server resolves them.
 */
data class Link(
    val platform: Platform,
    val nativeId: String?,
    val key: String,
    val url: String,
) {
    val isShort: Boolean get() = nativeId == null

    /** `<platform>:<native_id>`, or null for short links. */
    val videoId: String? get() = nativeId?.let { "${platform.id}:$it" }
}

/** Why a text has no usable link; [code] is the public API error code. */
enum class LinkRejection(val code: String) {
    UnsupportedUrl("unsupported_url"),
    UnsupportedContent("unsupported_content"),
}

sealed interface LinkResult {
    data class Supported(val link: Link) : LinkResult
    data class Rejected(val reason: LinkRejection) : LinkResult
}
