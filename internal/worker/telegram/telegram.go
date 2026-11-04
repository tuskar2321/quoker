// Package telegram is a minimal Bot API client for the worker: sendVideo and
// arbitrary calls for bot_call tasks (docs/06-telegram-bot.md).
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/tuskar2321/quoker/internal/shared/errs"
)

// DefaultBase is the cloud Bot API endpoint.
const DefaultBase = "https://api.telegram.org"

const maxResponseBytes = 4 << 20

// Client calls the Bot API. The token is passed per call because the core
// may rotate it through Config at any time.
type Client struct {
	hc *http.Client
}

// New returns a Client; hc must not have a global timeout, uploads are bounded
// by the caller's context.
func New(hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{}
	}
	return &Client{hc: hc}
}

// Error is a Bot API error response.
type Error struct {
	Status      int // Bot API error_code
	Description string
	RetryAfter  int // seconds, for 429
}

func (e *Error) Error() string {
	return fmt.Sprintf("telegram %d: %s", e.Status, e.Description)
}

// Code maps the response to a worker error code (docs/04-core-worker-protocol.md).
func (e *Error) Code() errs.Code {
	d := strings.ToLower(e.Description)
	switch {
	case e.Status == http.StatusTooManyRequests:
		return errs.RateLimited
	case e.Status == http.StatusForbidden:
		return errs.TargetForbidden
	case e.Status == http.StatusRequestEntityTooLarge || strings.Contains(d, "too large"):
		return errs.TooLarge
	case e.Status == http.StatusBadRequest && (strings.Contains(d, "chat not found") || strings.Contains(d, "upgraded to a supergroup")):
		return errs.TargetInactive
	case e.Status == http.StatusUnauthorized || e.Status == http.StatusNotFound:
		return errs.Internal // bad or revoked token
	}
	return errs.UpstreamError
}

// Call invokes method with a JSON body and returns the "result" field.
func (c *Client) Call(ctx context.Context, base, token, method string, params []byte) (json.RawMessage, error) {
	if len(params) == 0 {
		params = []byte("{}")
	}
	req, err := c.newRequest(ctx, base, token, method, bytes.NewReader(params))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req)
}

// Video describes a sendVideo call.
type Video struct {
	ChatID      int64
	CaptionHTML string // empty means no caption
	Path        string
	Thumb       []byte // optional JPEG
	DurationS   int
	Width       int
	Height      int
	// Progress, when set, receives the number of video bytes sent so far.
	Progress func(sent, total int64)
}

// SendVideo uploads a local file and returns the message id.
func (c *Client) SendVideo(ctx context.Context, base, token string, v Video) (int64, error) {
	f, err := os.Open(v.Path)
	if err != nil {
		return 0, fmt.Errorf("open video: %w", err)
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return 0, fmt.Errorf("stat video: %w", err)
	}

	// Build everything except the file bytes up front so the request has a
	// Content-Length and the video streams from disk without buffering.
	var head, tail bytes.Buffer
	sw := &switchWriter{w: &head}
	mw := multipart.NewWriter(sw)
	fields := [][2]string{
		{"chat_id", strconv.FormatInt(v.ChatID, 10)},
		{"supports_streaming", "true"},
		{"disable_notification", "false"},
	}
	if v.CaptionHTML != "" {
		fields = append(fields, [2]string{"caption", v.CaptionHTML}, [2]string{"parse_mode", "HTML"})
	}
	for _, kv := range [][2]any{{"duration", v.DurationS}, {"width", v.Width}, {"height", v.Height}} {
		if n := kv[1].(int); n > 0 {
			fields = append(fields, [2]string{kv[0].(string), strconv.Itoa(n)})
		}
	}
	if len(v.Thumb) > 0 {
		fields = append(fields, [2]string{"thumbnail", "attach://thumb"})
	}
	for _, kv := range fields {
		if err := mw.WriteField(kv[0], kv[1]); err != nil {
			return 0, err
		}
	}
	if len(v.Thumb) > 0 {
		pw, err := mw.CreateFormFile("thumb", "thumb.jpg")
		if err != nil {
			return 0, err
		}
		if _, err := pw.Write(v.Thumb); err != nil {
			return 0, err
		}
	}
	if _, err := mw.CreateFormFile("video", "video.mp4"); err != nil {
		return 0, err
	}
	sw.w = &tail
	if err := mw.Close(); err != nil {
		return 0, err
	}

	total := st.Size()
	body := io.MultiReader(&head, &progressReader{r: f, total: total, fn: v.Progress}, &tail)
	req, err := c.newRequest(ctx, base, token, "sendVideo", body)
	if err != nil {
		return 0, err
	}
	req.ContentLength = int64(head.Len()) + total + int64(tail.Len())
	req.Header.Set("Content-Type", mw.FormDataContentType())

	raw, err := c.do(req)
	if err != nil {
		return 0, err
	}
	var msg struct {
		MessageID int64 `json:"message_id"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil || msg.MessageID == 0 {
		return 0, errs.New(errs.UpstreamError, "sendVideo: no message_id in response")
	}
	return msg.MessageID, nil
}

func (c *Client) newRequest(ctx context.Context, base, token, method string, body io.Reader) (*http.Request, error) {
	if token == "" {
		return nil, errs.New(errs.Internal, "bot token is not configured")
	}
	if base == "" {
		base = DefaultBase
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(base, "/")+"/bot"+token+"/"+method, body)
	if err != nil {
		return nil, errs.New(errs.Internal, "build telegram request for "+method) // the URL holds the token
	}
	return req, nil
}

func (c *Client) do(req *http.Request) (json.RawMessage, error) {
	res, err := c.hc.Do(req)
	if err != nil {
		return nil, transportError(req.Context(), err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes))
	if err != nil {
		return nil, transportError(req.Context(), err)
	}
	var r struct {
		OK          bool            `json:"ok"`
		Result      json.RawMessage `json:"result"`
		ErrorCode   int             `json:"error_code"`
		Description string          `json:"description"`
		Parameters  struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, &Error{Status: res.StatusCode, Description: http.StatusText(res.StatusCode)}
	}
	if !r.OK {
		if r.ErrorCode == 0 {
			r.ErrorCode = res.StatusCode
		}
		return nil, &Error{Status: r.ErrorCode, Description: r.Description, RetryAfter: r.Parameters.RetryAfter}
	}
	return r.Result, nil
}

// transportError drops the request URL, which contains the bot token, from
// net/http errors.
func transportError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return errs.Wrap(errs.Cancelled, ctx.Err())
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return errs.Wrap(errs.UpstreamError, fmt.Errorf("telegram request: %w", err))
}

type switchWriter struct{ w io.Writer }

func (s *switchWriter) Write(p []byte) (int, error) { return s.w.Write(p) }

type progressReader struct {
	r     io.Reader
	sent  int64
	total int64
	fn    func(sent, total int64)
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.sent += int64(n)
	if p.fn != nil && n > 0 {
		p.fn(p.sent, p.total)
	}
	return n, err
}

// Update is one raw update from getUpdates.
type Update struct {
	ID   int64
	JSON []byte
}

// AllowedUpdates are the update kinds the core handles (docs/04-core-worker-protocol.md).
var AllowedUpdates = []string{"message", "my_chat_member", "chat_member", "callback_query"}

// GetUpdates long-polls for updates starting at offset.
func (c *Client) GetUpdates(ctx context.Context, base, token string, offset int64, timeoutS int) ([]Update, error) {
	params, err := json.Marshal(map[string]any{"offset": offset, "timeout": timeoutS, "allowed_updates": AllowedUpdates})
	if err != nil {
		return nil, err
	}
	raw, err := c.Call(ctx, base, token, "getUpdates", params)
	if err != nil {
		return nil, err
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, errs.New(errs.UpstreamError, "getUpdates: result is not an array")
	}
	out := make([]Update, 0, len(items))
	for _, it := range items {
		var head struct {
			UpdateID int64 `json:"update_id"`
		}
		if err := json.Unmarshal(it, &head); err != nil || head.UpdateID == 0 {
			continue
		}
		out = append(out, Update{ID: head.UpdateID, JSON: it})
	}
	return out, nil
}

var validFilePath = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_./-]{0,255}$`)

// DownloadFile fetches a file_path returned by getFile, up to maxBytes.
func (c *Client) DownloadFile(ctx context.Context, base, token, filePath string, maxBytes int64) ([]byte, error) {
	if token == "" {
		return nil, errs.New(errs.Internal, "bot token is not configured")
	}
	if !validFilePath.MatchString(filePath) || strings.Contains(filePath, "..") {
		return nil, errs.New(errs.Internal, "malformed file_path")
	}
	if base == "" {
		base = DefaultBase
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(base, "/")+"/file/bot"+token+"/"+filePath, nil)
	if err != nil {
		return nil, errs.New(errs.Internal, "build file request") // the URL holds the token
	}
	res, err := c.hc.Do(req)
	if err != nil {
		return nil, transportError(ctx, err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return nil, &Error{Status: res.StatusCode, Description: "file download: " + http.StatusText(res.StatusCode)}
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, maxBytes+1))
	if err != nil {
		return nil, transportError(ctx, err)
	}
	if int64(len(b)) > maxBytes {
		return nil, errs.New(errs.TooLarge, "file is larger than the limit")
	}
	return b, nil
}
