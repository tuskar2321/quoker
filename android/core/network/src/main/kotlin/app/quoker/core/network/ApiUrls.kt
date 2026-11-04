package app.quoker.core.network

import app.quoker.core.common.AppInfo
import javax.inject.Inject
import javax.inject.Singleton
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrl

/** URL helpers for the endpoints that are loaded directly (thumbnails, files) rather than via Retrofit. */
@Singleton
class ApiUrls @Inject constructor(appInfo: AppInfo) {
    /** API base URL, always with a trailing slash (Retrofit requires it). */
    val baseUrl: HttpUrl = normalizeBaseUrl(appInfo.apiBaseUrl)

    /** Resolves a relative API path such as `thumbnail_url` (`/v1/videos/…/thumbnail`) against [baseUrl]. */
    fun absolute(pathOrUrl: String): String {
        if (pathOrUrl.startsWith("http://") || pathOrUrl.startsWith("https://")) return pathOrUrl
        return requireNotNull(baseUrl.resolve(pathOrUrl.removePrefix("/"))) { "bad path: $pathOrUrl" }.toString()
    }

    fun thumbnail(videoId: String): String = videoUrl(videoId, "thumbnail")

    /** `GET /v1/videos/{id}/file`, supports `Range`. */
    fun file(videoId: String): String = videoUrl(videoId, "file")

    /** True when [url] points at the API origin; only such requests get session headers. */
    fun isApiOrigin(url: HttpUrl): Boolean =
        url.scheme == baseUrl.scheme && url.host == baseUrl.host && url.port == baseUrl.port

    private fun videoUrl(videoId: String, leaf: String): String = baseUrl.newBuilder()
        .addPathSegment("v1")
        .addPathSegment("videos")
        .addPathSegment(videoId)
        .addPathSegment(leaf)
        .build()
        .toString()

    companion object {
        fun normalizeBaseUrl(raw: String): HttpUrl = (if (raw.endsWith("/")) raw else "$raw/").toHttpUrl()
    }
}
