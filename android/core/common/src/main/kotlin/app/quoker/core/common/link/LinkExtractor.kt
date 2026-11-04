package app.quoker.core.common.link

/**
 * Kotlin port of the core's internal/shared/urls (docs/07-downloading.md, "Нормализация ссылок").
 * Behaviour is pinned by the shared vectors.json; keep both in sync.
 */
object LinkExtractor {
    /** Longest URL accepted, in UTF-8 bytes (docs/11-nfr-security.md). */
    const val MAX_LEN = 2048

    private val candidateRe = Regex("""(?i)https?://[^\t\n\u000C\r <>"'«»]+""")
    private val digitsRe = Regex("^[0-9]+$")
    private val shortCodeRe = Regex("^[A-Za-z0-9]+$")
    private val igCodeRe = Regex("^[A-Za-z0-9_-]+$")
    private val ttUserRe = Regex("^@[A-Za-z0-9._-]*$")
    private val trailingPunct = ".,;:!?)]}…".toCharArray()

    /** First supported link in [text], or null if there is none or it is not a supported video. */
    fun firstSupported(text: String): Link? = (extract(text) as? LinkResult.Supported)?.link

    /**
     * Finds the first URL of a supported domain in free text and normalizes it. URLs of other
     * domains before it are skipped; if the first supported-domain URL is not a supported video,
     * its rejection is returned and later URLs are not considered.
     */
    fun extract(text: String): LinkResult {
        for (match in candidateRe.findAll(text)) {
            val candidate = match.value.trimEnd(*trailingPunct)
            val parsed = GoUrl.parse(candidate) ?: continue
            if (supportedHost(parsed.hostname) == null) continue
            return normalize(candidate)
        }
        return LinkResult.Rejected(LinkRejection.UnsupportedUrl)
    }

    /** Recognizes a single URL. */
    fun normalize(raw: String): LinkResult {
        if (raw.toByteArray(Charsets.UTF_8).size > MAX_LEN) return unsupportedUrl
        val u = GoUrl.parse(raw) ?: return unsupportedUrl
        if ((u.scheme != "http" && u.scheme != "https") || u.hasUserInfo || u.port.isNotEmpty()) {
            return unsupportedUrl
        }
        val host = supportedHost(u.hostname) ?: return unsupportedUrl
        val segs = u.path.split('/').filter { it.isNotEmpty() }
        return when (host) {
            "tiktok.com" -> tiktok(segs)
            "vm.tiktok.com", "vt.tiktok.com" ->
                if (segs.size == 1 && shortCodeRe.matches(segs[0])) {
                    supported(Platform.TikTok, null, "https://$host/${segs[0]}", "https://$host/${segs[0]}/")
                } else {
                    unsupportedUrl
                }
            "instagram.com" -> instagram(segs)
            else -> unsupportedUrl
        }
    }

    private fun supportedHost(h: String): String? {
        val host = h.lowercase().removePrefix("www.").removePrefix("m.")
        return when (host) {
            "tiktok.com", "vm.tiktok.com", "vt.tiktok.com", "instagram.com" -> host
            else -> null
        }
    }

    private fun tiktok(segs: List<String>): LinkResult {
        if (segs.size < 2) return unsupportedUrl
        val first = segs[0]
        val second = segs[1]
        val rest = segs.drop(2)
        if (ttUserRe.matches(first) && (second == "photo" || second == "live")) return unsupportedContent
        return when {
            rest.isEmpty() && first == "v" -> tiktokById(second.removeSuffix(".html"))
            rest.isEmpty() && first == "t" && shortCodeRe.matches(second) ->
                supported(Platform.TikTok, null, "https://tiktok.com/t/$second", "https://www.tiktok.com/t/$second/")
            // The user-less form produced by tiktokById; same key as /v/<id>.
            rest.size == 1 && second == "video" && first == "@" -> tiktokById(rest[0])
            rest.size == 1 && second == "video" && ttUserRe.matches(first) && digitsRe.matches(rest[0]) -> {
                val id = rest[0]
                supported(
                    Platform.TikTok,
                    id,
                    "https://tiktok.com/$first/video/$id",
                    "https://www.tiktok.com/$first/video/$id",
                )
            }
            else -> unsupportedUrl
        }
    }

    private fun tiktokById(id: String): LinkResult {
        if (!digitsRe.matches(id)) return unsupportedUrl
        return supported(Platform.TikTok, id, "https://tiktok.com/v/$id", "https://www.tiktok.com/@/video/$id")
    }

    private fun instagram(segs: List<String>): LinkResult {
        if (segs.isEmpty()) return unsupportedUrl
        return when (segs[0]) {
            "stories", "tv" -> unsupportedContent
            "reel", "reels", "p" ->
                if (segs.size == 2 && igCodeRe.matches(segs[1]) && segs[1] != "audio") {
                    supported(
                        Platform.Instagram,
                        segs[1],
                        "https://instagram.com/${segs[0]}/${segs[1]}",
                        "https://www.instagram.com/${segs[0]}/${segs[1]}/",
                    )
                } else {
                    unsupportedUrl
                }
            else -> unsupportedUrl
        }
    }

    private fun supported(platform: Platform, nativeId: String?, key: String, url: String) =
        LinkResult.Supported(Link(platform, nativeId, key, url))

    private val unsupportedUrl = LinkResult.Rejected(LinkRejection.UnsupportedUrl)
    private val unsupportedContent = LinkResult.Rejected(LinkRejection.UnsupportedContent)
}

/**
 * The subset of Go's net/url.Parse that the rules depend on: scheme (lower-cased), userinfo
 * presence, host/port split and the percent-decoded path. Returns null where Go returns an error.
 */
internal data class GoUrl(
    val scheme: String,
    val hasUserInfo: Boolean,
    val hostname: String,
    val port: String,
    val path: String,
) {
    companion object {
        private val schemeRe = Regex("^([A-Za-z][A-Za-z0-9+.-]*):")
        private const val USERINFO_EXTRA = "-._:~!$&'()*+,;=%@"
        private const val HOST_EXTRA = "-_.~!$&'()*+,;=:[]<>\""

        fun parse(raw: String): GoUrl? {
            val beforeFragment = raw.substringBefore('#')
            if (beforeFragment.any { it.code < 0x20 || it.code == 0x7f }) return null
            if (beforeFragment.startsWith(":")) return null
            val schemeMatch = schemeRe.find(beforeFragment) ?: return null
            val scheme = schemeMatch.groupValues[1].lowercase()
            var rest = beforeFragment.substring(schemeMatch.range.last + 1)
            rest = rest.substringBefore('?')
            if (!rest.startsWith("//")) return null
            val afterSlashes = rest.substring(2)
            val slash = afterSlashes.indexOf('/')
            val authority = if (slash < 0) afterSlashes else afterSlashes.substring(0, slash)
            val rawPath = if (slash < 0) "" else afterSlashes.substring(slash)

            val at = authority.lastIndexOf('@')
            val hasUserInfo = at >= 0
            if (hasUserInfo) {
                val userinfo = authority.substring(0, at)
                if (userinfo.any { !(it.isAsciiLetterOrDigit() || it in USERINFO_EXTRA) }) return null
            }
            val hostPort = authority.substring(at + 1)
            if (hostPort.startsWith("[")) return null // IPv6 literals are never supported hosts.
            val colon = hostPort.lastIndexOf(':')
            val rawHost: String
            val port: String
            if (colon >= 0) {
                port = hostPort.substring(colon + 1)
                if (port.any { it !in '0'..'9' }) return null
                rawHost = hostPort.substring(0, colon)
            } else {
                port = ""
                rawHost = hostPort
            }
            if (rawHost.any { it.code < 0x80 && !(it.isAsciiLetterOrDigit() || it in HOST_EXTRA || it == '%') }) {
                return null
            }
            val host = percentDecode(rawHost) ?: return null
            val path = percentDecode(rawPath) ?: return null
            return GoUrl(scheme, hasUserInfo, host, port, path)
        }

        private fun Char.isAsciiLetterOrDigit() = this in 'a'..'z' || this in 'A'..'Z' || this in '0'..'9'

        private fun percentDecode(s: String): String? {
            if ('%' !in s) return s
            val src = s.toByteArray(Charsets.UTF_8)
            val out = java.io.ByteArrayOutputStream(src.size)
            var i = 0
            while (i < src.size) {
                val b = src[i]
                if (b == '%'.code.toByte()) {
                    if (i + 2 >= src.size) return null
                    val hi = Character.digit(src[i + 1].toInt().toChar(), 16)
                    val lo = Character.digit(src[i + 2].toInt().toChar(), 16)
                    if (hi < 0 || lo < 0) return null
                    out.write(hi * 16 + lo)
                    i += 3
                } else {
                    out.write(b.toInt())
                    i++
                }
            }
            return out.toByteArray().toString(Charsets.UTF_8)
        }
    }
}
