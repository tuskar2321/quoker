package link

import (
	"net/url"
	"strings"

	"google.golang.org/protobuf/proto"

	linkv1 "github.com/tuskar2321/quoker/gen/quoker/link/v1"
)

// TaskPlatform names what a task talks to: "tiktok" or "instagram" for
// resolve and download (by the video URL), "telegram" for send_video, ""
// otherwise. bot_call is left out: its refusals are mostly ordinary answers
// to rights checks, not Telegram trouble.
func TaskPlatform(kind string, payload []byte) string {
	var raw string
	switch kind {
	case "send_video":
		return "telegram"
	case "bot_call":
		// Refusals here are mostly ordinary answers to rights checks
		// ("user not found"), not Telegram trouble.
		return ""
	case "resolve":
		var t linkv1.ResolveTask
		if proto.Unmarshal(payload, &t) == nil {
			raw = t.Url
		}
	case "download":
		var t linkv1.DownloadTask
		if proto.Unmarshal(payload, &t) == nil {
			raw = t.Url
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	switch h := u.Hostname(); {
	case strings.HasSuffix(h, "tiktok.com"):
		return "tiktok"
	case strings.HasSuffix(h, "instagram.com"):
		return "instagram"
	}
	return ""
}
