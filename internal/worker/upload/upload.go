// Package upload sends files and thumbnails from the worker to the core over
// mTLS HTTPS, resuming interrupted uploads (docs/04-core-worker-protocol.md).
package upload

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/tuskar2321/quoker/internal/shared/errs"
)

const (
	attempts     = 3
	retryPause   = 2 * time.Second
	thumbTimeout = 30 * time.Second
)

// Client uploads to the core's internal endpoints.
type Client struct {
	hc    *http.Client
	pause time.Duration
}

// New returns a Client; hc carries the worker's client certificate.
func New(hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{}
	}
	return &Client{hc: hc, pause: retryPause}
}

// WithRetryPause overrides the pause between attempts (for tests).
func (c *Client) WithRetryPause(d time.Duration) *Client {
	c.pause = d
	return c
}

// PutThumb uploads a JPEG thumbnail.
func (c *Client) PutThumb(ctx context.Context, base, videoID string, jpeg []byte) error {
	ctx, cancel := context.WithTimeout(ctx, thumbTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint(base, "thumbs", videoID), bytes.NewReader(jpeg))
	if err != nil {
		return errs.Wrap(errs.Internal, err)
	}
	req.Header.Set("Content-Type", "image/jpeg")
	res, err := c.hc.Do(req)
	if err != nil {
		return transportError(ctx, err)
	}
	_ = res.Body.Close()
	return statusError(res)
}

// PutFile uploads path, continuing from the size the core already holds.
// HEAD answers with Content-Length of the bytes received so far (404: none).
func (c *Client) PutFile(ctx context.Context, base, videoID, path string, progress func(sent, total int64)) error {
	f, err := os.Open(path) //nolint:gosec // path comes from the worker's own cache
	if err != nil {
		return fmt.Errorf("open upload: %w", err)
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat upload: %w", err)
	}
	total := st.Size()
	u := endpoint(base, "files", videoID)

	var lastErr error
	for attempt := range attempts {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return errs.Wrap(errs.Cancelled, ctx.Err())
			case <-time.After(c.pause):
			}
		}
		offset, err := c.received(ctx, u)
		if err != nil {
			lastErr = err
			if retryable(err) {
				continue
			}
			return err
		}
		if offset >= total {
			return nil
		}
		lastErr = c.putRange(ctx, u, f, offset, total, progress)
		if lastErr == nil || !retryable(lastErr) {
			return lastErr
		}
	}
	return lastErr
}

func (c *Client) received(ctx context.Context, u string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, u, nil)
	if err != nil {
		return 0, errs.Wrap(errs.Internal, err)
	}
	res, err := c.hc.Do(req)
	if err != nil {
		return 0, transportError(ctx, err)
	}
	_ = res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return 0, nil
	}
	if err := statusError(res); err != nil {
		return 0, err
	}
	n, err := strconv.ParseInt(res.Header.Get("Content-Length"), 10, 64)
	if err != nil || n < 0 {
		return 0, errs.New(errs.UpstreamError, "HEAD without Content-Length")
	}
	return n, nil
}

func (c *Client) putRange(ctx context.Context, u string, f *os.File, offset, total int64, progress func(sent, total int64)) error {
	body := &countingReader{r: io.NewSectionReader(f, offset, total-offset), sent: offset, total: total, fn: progress}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, body)
	if err != nil {
		return errs.Wrap(errs.Internal, err)
	}
	req.ContentLength = total - offset
	req.Header.Set("Content-Type", "video/mp4")
	req.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offset, total-1, total))
	res, err := c.hc.Do(req)
	if err != nil {
		return transportError(ctx, err)
	}
	_ = res.Body.Close()
	if res.StatusCode == http.StatusRequestedRangeNotSatisfiable || res.StatusCode == http.StatusConflict {
		return errs.New(errs.UpstreamError, "offset mismatch, re-checking") // next attempt re-reads HEAD
	}
	return statusError(res)
}

func endpoint(base, kind, videoID string) string {
	return strings.TrimRight(base, "/") + "/" + kind + "/" + url.PathEscape(videoID)
}

func statusError(res *http.Response) error {
	switch {
	case res.StatusCode < 300:
		return nil
	case res.StatusCode >= 500 || res.StatusCode == http.StatusTooManyRequests:
		return errs.New(errs.UpstreamError, "core responded "+res.Status)
	}
	return errs.New(errs.Internal, "core rejected upload: "+res.Status)
}

func transportError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return errs.Wrap(errs.Cancelled, ctx.Err())
	}
	return errs.Wrap(errs.UpstreamError, err)
}

func retryable(err error) bool { return errs.Is(err, errs.UpstreamError) }

type countingReader struct {
	r     io.Reader
	sent  int64
	total int64
	fn    func(sent, total int64)
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.sent += int64(n)
	if c.fn != nil && n > 0 {
		c.fn(c.sent, c.total)
	}
	return n, err
}
