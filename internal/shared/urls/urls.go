// Package urls extracts and normalizes TikTok and Instagram links.
//
// The rules are described in docs/07-downloading.md. The behaviour is pinned
// by testdata/vectors.json, which the Android client reuses for its own
// client-side check, so any change here must keep the vectors in sync.
package urls

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

// Platform is a supported video platform.
type Platform string

// Supported platforms; the values are the video_id prefixes.
const (
	TikTok    Platform = "tiktok"
	Instagram Platform = "instagram"
)

// MaxLen is the longest URL accepted (docs/11-nfr-security.md).
const MaxLen = 2048

// Errors match the public API codes in docs/03-public-api.md.
var (
	ErrUnsupportedURL     = errors.New("unsupported_url")
	ErrUnsupportedContent = errors.New("unsupported_content")
)

// Link is a recognized, normalized link.
type Link struct {
	Platform Platform
	// NativeID is the platform video id; empty for short links, whose id is
	// only known after the worker resolves them.
	NativeID string
	// Key is the normalized form (lower-case host without www./m., no query
	// or fragment, no trailing slash). Used for dedup and video_aliases.
	Key string
	// URL is the canonical URL handed to yt-dlp. It may differ from Key
	// because yt-dlp extractors require specific hosts (www.tiktok.com).
	URL string
}

// Short reports whether the link must be resolved over the network to learn
// the video id.
func (l Link) Short() bool { return l.NativeID == "" }

// VideoID returns "<platform>:<native_id>", or "" for short links.
func (l Link) VideoID() string {
	if l.Short() {
		return ""
	}
	return string(l.Platform) + ":" + l.NativeID
}

var (
	candidateRe = regexp.MustCompile(`(?i)https?://[^\s<>"'«»]+`)
	digitsRe    = regexp.MustCompile(`^[0-9]+$`)
	shortCodeRe = regexp.MustCompile(`^[A-Za-z0-9]+$`)
	igCodeRe    = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	ttUserRe    = regexp.MustCompile(`^@[A-Za-z0-9._-]*$`)
)

const trailingPunct = ".,;:!?)]}…"

// Parse finds the first URL of a supported domain in free text (TikTok and
// Instagram prepend their own text when sharing) and normalizes it.
// URLs of other domains before it are skipped; if the first supported-domain
// URL is not a supported video, its error is returned.
func Parse(text string) (Link, error) {
	for _, c := range candidateRe.FindAllString(text, -1) {
		c = strings.TrimRight(c, trailingPunct)
		u, err := url.Parse(c)
		if err != nil {
			continue
		}
		if _, ok := supportedHost(u.Hostname()); !ok {
			continue
		}
		return Normalize(c)
	}
	return Link{}, ErrUnsupportedURL
}

// Normalize recognizes a single URL.
func Normalize(raw string) (Link, error) {
	if len(raw) > MaxLen {
		return Link{}, ErrUnsupportedURL
	}
	u, err := url.Parse(raw)
	if err != nil {
		return Link{}, ErrUnsupportedURL
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Port() != "" {
		return Link{}, ErrUnsupportedURL
	}
	host, ok := supportedHost(u.Hostname())
	if !ok {
		return Link{}, ErrUnsupportedURL
	}
	segs := segments(u.Path)
	switch host {
	case "tiktok.com":
		return tiktok(segs)
	case "vm.tiktok.com", "vt.tiktok.com":
		if len(segs) == 1 && shortCodeRe.MatchString(segs[0]) {
			return Link{
				Platform: TikTok,
				Key:      "https://" + host + "/" + segs[0],
				URL:      "https://" + host + "/" + segs[0] + "/",
			}, nil
		}
	case "instagram.com":
		return instagram(segs)
	}
	return Link{}, ErrUnsupportedURL
}

// supportedHost lower-cases the host, strips "www." and "m." and reports
// whether the result is a supported host.
func supportedHost(h string) (string, bool) {
	h = strings.ToLower(h)
	h = strings.TrimPrefix(h, "www.")
	h = strings.TrimPrefix(h, "m.")
	switch h {
	case "tiktok.com", "vm.tiktok.com", "vt.tiktok.com", "instagram.com":
		return h, true
	}
	return "", false
}

func segments(p string) []string {
	var out []string
	for s := range strings.SplitSeq(p, "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func tiktok(segs []string) (Link, error) {
	if len(segs) < 2 {
		return Link{}, ErrUnsupportedURL
	}
	first, second, rest := segs[0], segs[1], segs[2:]
	if ttUserRe.MatchString(first) && (second == "photo" || second == "live") {
		return Link{}, ErrUnsupportedContent
	}
	switch {
	case len(rest) == 0 && first == "v":
		return tiktokByID(strings.TrimSuffix(second, ".html"))
	case len(rest) == 0 && first == "t" && shortCodeRe.MatchString(second):
		return Link{
			Platform: TikTok,
			Key:      "https://tiktok.com/t/" + second,
			URL:      "https://www.tiktok.com/t/" + second + "/",
		}, nil
	case len(rest) == 1 && second == "video" && first == "@":
		// The user-less form produced by tiktokByID; same key as /v/<id>.
		return tiktokByID(rest[0])
	case len(rest) == 1 && second == "video" && ttUserRe.MatchString(first) && digitsRe.MatchString(rest[0]):
		id := rest[0]
		return Link{
			Platform: TikTok,
			NativeID: id,
			Key:      "https://tiktok.com/" + first + "/video/" + id,
			URL:      "https://www.tiktok.com/" + first + "/video/" + id,
		}, nil
	}
	return Link{}, ErrUnsupportedURL
}

// tiktokByID builds a link from a bare video id (the /v/<id> forms).
// yt-dlp does not recognize /v/<id>; the user part of /@user/video/<id> is
// optional there, so the fetch URL uses /@/video/<id>.
func tiktokByID(id string) (Link, error) {
	if !digitsRe.MatchString(id) {
		return Link{}, ErrUnsupportedURL
	}
	return Link{
		Platform: TikTok,
		NativeID: id,
		Key:      "https://tiktok.com/v/" + id,
		URL:      "https://www.tiktok.com/@/video/" + id,
	}, nil
}

func instagram(segs []string) (Link, error) {
	if len(segs) == 0 {
		return Link{}, ErrUnsupportedURL
	}
	switch segs[0] {
	case "stories", "tv":
		return Link{}, ErrUnsupportedContent
	case "reel", "reels", "p":
		if len(segs) == 2 && igCodeRe.MatchString(segs[1]) && segs[1] != "audio" {
			return Link{
				Platform: Instagram,
				NativeID: segs[1],
				Key:      "https://instagram.com/" + segs[0] + "/" + segs[1],
				URL:      "https://www.instagram.com/" + segs[0] + "/" + segs[1] + "/",
			}, nil
		}
	}
	return Link{}, ErrUnsupportedURL
}
