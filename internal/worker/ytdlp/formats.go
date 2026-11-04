package ytdlp

import (
	"math"
	"sort"
	"strconv"
	"strings"
)

// Info is the subset of "yt-dlp -J" output the worker uses. Numbers are
// float64 because extractors are inconsistent about integer fields.
type Info struct {
	ID           string   `json:"id"`
	Type         string   `json:"_type"`
	ExtractorKey string   `json:"extractor_key"`
	WebpageURL   string   `json:"webpage_url"`
	Uploader     string   `json:"uploader"`
	UploaderID   string   `json:"uploader_id"`
	Channel      string   `json:"channel"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	Thumbnail    string   `json:"thumbnail"`
	Duration     float64  `json:"duration"`
	Width        float64  `json:"width"`
	Height       float64  `json:"height"`
	Formats      []Format `json:"formats"`

	UsedCookies bool `json:"-"`
}

// Format is one entry of Info.Formats.
type Format struct {
	ID             string  `json:"format_id"`
	Note           string  `json:"format_note"`
	Ext            string  `json:"ext"`
	VCodec         string  `json:"vcodec"`
	ACodec         string  `json:"acodec"`
	Width          float64 `json:"width"`
	Height         float64 `json:"height"`
	Filesize       float64 `json:"filesize"`
	FilesizeApprox float64 `json:"filesize_approx"`
	TBR            float64 `json:"tbr"`
	ABR            float64 `json:"abr"`
}

// Platform returns "tiktok", "instagram" or "".
func (i *Info) Platform() string {
	k := strings.ToLower(i.ExtractorKey)
	switch {
	case strings.HasPrefix(k, "tiktok"):
		return "tiktok"
	case strings.HasPrefix(k, "instagram"):
		return "instagram"
	}
	return ""
}

// HasVideo reports whether any format carries a video track.
func (i *Info) HasVideo() bool {
	for _, f := range i.Formats {
		if f.hasVideo() {
			return true
		}
	}
	return false
}

// HasCleanVideo reports whether a video format without a watermark exists.
func (i *Info) HasCleanVideo() bool {
	for _, f := range i.Formats {
		if f.hasVideo() && !f.Watermarked() {
			return true
		}
	}
	return false
}

// Watermarked reports whether TikTok marked the format as carrying a watermark.
func (f Format) Watermarked() bool {
	return strings.Contains(strings.ToLower(f.Note), "watermark") ||
		strings.HasPrefix(strings.ToLower(f.ID), "download")
}

func (f Format) hasVideo() bool { return f.VCodec != "none" && (f.VCodec != "" || f.Height > 0) }
func (f Format) hasAudio() bool { return f.ACodec != "none" }
func (f Format) height() int    { return int(f.Height) }

func (f Format) size() int64 {
	if f.Filesize > 0 {
		return int64(f.Filesize)
	}
	return int64(f.FilesizeApprox)
}

func (f Format) isH264() bool {
	c := strings.ToLower(f.VCodec)
	return strings.HasPrefix(c, "avc") || strings.HasPrefix(c, "h264")
}

func (f Format) isAAC() bool {
	c := strings.ToLower(f.ACodec)
	return strings.HasPrefix(c, "mp4a") || strings.HasPrefix(c, "aac") || f.Ext == "m4a"
}

// Choice is a selected format combination.
type Choice struct {
	Selector string // value for "yt-dlp -f"
	Height   int    // 0 when unknown
	Width    int
	EstBytes int64 // 0 when unknown
}

// Choose picks the best video with height ≤ maxHeight whose known size fits
// maxBytes, preferring H.264 within the same height and pairing video-only
// formats with the best audio. Watermarked formats are skipped when
// excludeWatermarked is set. ok is false when nothing fits.
func (i *Info) Choose(maxHeight int, maxBytes int64, excludeWatermarked bool) (c Choice, ok bool) {
	var videos []Format
	var audio *Format
	for k := range i.Formats {
		f := i.Formats[k]
		if !f.hasVideo() {
			if f.hasAudio() && f.ACodec != "" && (audio == nil || betterAudio(f, *audio)) {
				audio = &i.Formats[k]
			}
			continue
		}
		if excludeWatermarked && f.Watermarked() {
			continue
		}
		if f.height() > maxHeight {
			continue
		}
		videos = append(videos, f)
	}
	sort.SliceStable(videos, func(a, b int) bool {
		va, vb := videos[a], videos[b]
		if va.height() != vb.height() {
			return va.height() > vb.height()
		}
		if va.isH264() != vb.isH264() {
			return va.isH264()
		}
		return va.TBR > vb.TBR
	})
	for _, v := range videos {
		c := Choice{Selector: v.ID, Height: v.height(), Width: int(v.Width), EstBytes: v.size()}
		if !v.hasAudio() {
			if audio == nil {
				continue
			}
			c.Selector = v.ID + "+" + audio.ID
			if v.size() > 0 && audio.size() > 0 {
				c.EstBytes = v.size() + audio.size()
			} else {
				c.EstBytes = 0
			}
		}
		if c.EstBytes > 0 && c.EstBytes > maxBytes {
			continue
		}
		return c, true
	}
	return Choice{}, false
}

func betterAudio(a, b Format) bool {
	if a.isAAC() != b.isAAC() {
		return a.isAAC()
	}
	return math.Max(a.ABR, a.TBR) > math.Max(b.ABR, b.TBR)
}

// Ladder is the resolution sequence for fitting a Telegram upload.
var Ladder = []int{1080, 720, 540, 480}

// NextHeight returns the next ladder step strictly below h, or 0.
func NextHeight(h int) int {
	for _, s := range Ladder {
		if s < h {
			return s
		}
	}
	return 0
}

func (c Choice) String() string { return c.Selector + "@" + strconv.Itoa(c.Height) + "p" }
