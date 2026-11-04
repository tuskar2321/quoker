package tasks

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"strings"
	"time"

	// Decoders for the formats platforms serve thumbnails in.
	_ "image/gif"
	_ "image/png"

	// WebP decoder: TikTok serves covers as WebP.
	_ "golang.org/x/image/webp"

	"golang.org/x/image/draw"
)

const (
	thumbFetchTimeout = 20 * time.Second
	// coverBudget bounds fetching and uploading the cover during resolve.
	coverBudget      = 10 * time.Second
	thumbMaxDownload = 10 << 20
	thumbMaxPixels   = 40_000_000 // guards against decompression bombs

	// Cover stored by the core (docs/07-downloading.md).
	coverSide  = 720
	coverBytes = 512 << 10
	// Preview attached to sendVideo.
	previewSide  = 320
	previewBytes = 200 << 10
)

func (h *Handlers) fetchImage(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, thumbFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("thumbnail request: %w", err)
	}
	if h.d.UserAgent != "" {
		req.Header.Set("User-Agent", h.d.UserAgent)
	}
	res, err := h.d.Web.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch thumbnail: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch thumbnail: %s", res.Status)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, thumbMaxDownload+1))
	if err != nil {
		return nil, fmt.Errorf("read thumbnail: %w", err)
	}
	if len(b) > thumbMaxDownload {
		return nil, fmt.Errorf("thumbnail larger than %d bytes", thumbMaxDownload)
	}
	return b, nil
}

// makeJPEG scales src so its long side is at most maxSide and encodes it as
// JPEG no larger than maxBytes, lowering quality and then size as needed.
func makeJPEG(src []byte, maxSide, maxBytes int) ([]byte, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(src))
	if err != nil {
		return nil, fmt.Errorf("thumbnail config: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > thumbMaxPixels {
		return nil, fmt.Errorf("thumbnail is %dx%d", cfg.Width, cfg.Height)
	}
	img, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return nil, fmt.Errorf("decode thumbnail: %w", err)
	}
	for side := maxSide; side >= 64; side /= 2 {
		scaled := scale(img, side)
		for q := 90; q >= 40; q -= 10 {
			var buf bytes.Buffer
			if err := jpeg.Encode(&buf, scaled, &jpeg.Options{Quality: q}); err != nil {
				return nil, fmt.Errorf("encode thumbnail: %w", err)
			}
			if buf.Len() <= maxBytes {
				return buf.Bytes(), nil
			}
		}
	}
	return nil, fmt.Errorf("thumbnail does not fit %d bytes", maxBytes)
}

func scale(img image.Image, maxSide int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxSide && h <= maxSide {
		return img
	}
	if w >= h {
		h = h * maxSide / w
		w = maxSide
	} else {
		w = w * maxSide / h
		h = maxSide
	}
	dst := image.NewRGBA(image.Rect(0, 0, max(w, 1), max(h, 1)))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
	return dst
}

// thumbnail fetches url and returns it prepared for the given limits.
func (h *Handlers) thumbnail(ctx context.Context, url string, side, limit int) ([]byte, error) {
	if !strings.HasPrefix(url, "https://") {
		return nil, fmt.Errorf("thumbnail url is not https")
	}
	raw, err := h.fetchImage(ctx, url)
	if err != nil {
		return nil, err
	}
	return makeJPEG(raw, side, limit)
}
