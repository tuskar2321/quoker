// Package ytdlp runs yt-dlp as an external process and interprets its output.
// See docs/07-downloading.md.
package ytdlp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/tuskar2321/quoker/internal/shared/errs"
)

const (
	resolveTimeout  = 45 * time.Second
	downloadTimeout = 8 * time.Minute
	versionTimeout  = 10 * time.Second
	outputLimit     = 1 << 20
	stderrInMessage = 300
)

// Runner invokes yt-dlp.
type Runner struct {
	bin       string
	prefix    []string
	userAgent string
	tmpRoot   string
}

// New returns a Runner. tmpRoot holds per-call scratch directories (cookies,
// downloads). prefixArgs are placed before yt-dlp's own arguments; tests use
// them to run a fake binary.
func New(bin, userAgent, tmpRoot string, prefixArgs ...string) *Runner {
	return &Runner{bin: bin, prefix: prefixArgs, userAgent: userAgent, tmpRoot: tmpRoot}
}

// Version returns the yt-dlp version string.
func (r *Runner) Version(ctx context.Context) (string, error) {
	out, _, err := r.run(ctx, versionTimeout, "", []string{"--version"})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Metadata runs "yt-dlp -J". cookies (Netscape format) are used only for
// Instagram and only after a first attempt without them asked for a login.
func (r *Runner) Metadata(ctx context.Context, rawURL, cookies string) (*Info, error) {
	var info *Info
	used, err := r.withCookies(rawURL, cookies, func(extra []string) error {
		out, _, err := r.run(ctx, resolveTimeout, "", append(append(r.common(), extra...), "-J", "--", rawURL))
		if err != nil {
			return err
		}
		var i Info
		if err := json.Unmarshal(out, &i); err != nil {
			return errs.Wrap(errs.UpstreamError, fmt.Errorf("parse yt-dlp json: %w", err))
		}
		info = &i
		return nil
	})
	if err != nil {
		return nil, err
	}
	info.UsedCookies = used
	return info, nil
}

// Download fetches the formats chosen by selector into dir and returns the
// path of the resulting file. With maxBytes > 0 yt-dlp aborts formats larger
// than that instead of downloading them whole; the error is then too_large.
func (r *Runner) Download(ctx context.Context, rawURL, selector, dir, cookies string, maxBytes int64) (string, error) {
	var path string
	_, err := r.withCookies(rawURL, cookies, func(extra []string) error {
		args := append(r.common(), extra...)
		args = append(args,
			"-f", selector,
			"--merge-output-format", "mp4",
			"-o", filepath.Join(dir, "%(id)s.%(ext)s"),
			"--print", "after_move:filepath")
		if maxBytes > 0 {
			args = append(args, "--max-filesize", strconv.FormatInt(maxBytes, 10))
		}
		args = append(args, "--", rawURL)
		out, errOut, err := r.run(ctx, downloadTimeout, dir, args)
		if err != nil {
			return err
		}
		// yt-dlp skips an oversized format with exit code 0 and no file.
		if bytes.Contains(out, []byte("max-filesize")) || bytes.Contains(errOut, []byte("max-filesize")) {
			return errs.New(errs.TooLarge, "format exceeds --max-filesize")
		}
		path, err = outputFile(dir, out)
		return err
	})
	return path, err
}

func (r *Runner) common() []string {
	args := []string{
		"--ignore-config", "--no-playlist", "--no-warnings", "--no-progress",
		"--socket-timeout", "20", "--retries", "2", "--no-cache-dir",
	}
	if r.userAgent != "" {
		args = append(args, "--user-agent", r.userAgent)
	}
	return args
}

// withCookies runs fn without cookies and, if the platform asked for a login
// and cookies are available, once more with a 0600 cookies file that is
// removed afterwards.
func (r *Runner) withCookies(rawURL, cookies string, fn func(extra []string) error) (used bool, err error) {
	err = fn(nil)
	if err == nil || !errs.Is(err, errs.LoginRequired) || cookies == "" || !isInstagram(rawURL) {
		return false, err
	}
	dir, err := os.MkdirTemp(r.tmpRoot, "cookies-")
	if err != nil {
		return false, fmt.Errorf("cookies dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	file := filepath.Join(dir, "cookies.txt")
	if err := os.WriteFile(file, []byte(cookies), 0o600); err != nil {
		return false, fmt.Errorf("write cookies: %w", err)
	}
	return true, fn([]string{"--cookies", file})
}

func (r *Runner) run(ctx context.Context, timeout time.Duration, dir string, args []string) (stdout, stderr []byte, err error) {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, r.bin, append(append([]string(nil), r.prefix...), args...)...) //nolint:gosec // args are an array, never a shell string
	cmd.Dir = dir
	out := &limitedBuffer{max: outputLimit}
	errOut := &limitedBuffer{max: outputLimit}
	cmd.Stdout, cmd.Stderr = out, errOut
	cmd.WaitDelay = 5 * time.Second
	setProcessGroup(cmd)

	runErr := cmd.Run()
	switch {
	case ctx.Err() != nil:
		return nil, nil, errs.Wrap(errs.Cancelled, ctx.Err())
	case runCtx.Err() != nil:
		return nil, nil, errs.New(errs.UpstreamError, fmt.Sprintf("yt-dlp timed out after %s", timeout))
	case runErr != nil:
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) {
			return nil, nil, errs.Wrap(errs.Internal, fmt.Errorf("start yt-dlp: %w", runErr))
		}
		return nil, nil, classify(errOut.String())
	}
	return out.Bytes(), errOut.Bytes(), nil
}

// outputFile returns the path printed by --print after_move:filepath, falling
// back to the only finished file in dir.
func outputFile(dir string, stdout []byte) (string, error) {
	lines := strings.Split(strings.TrimSpace(string(stdout)), "\n")
	if p := strings.TrimSpace(lines[len(lines)-1]); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read download dir: %w", err)
	}
	var found []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || strings.HasSuffix(n, ".part") || strings.HasSuffix(n, ".ytdl") {
			continue
		}
		found = append(found, filepath.Join(dir, n))
	}
	if len(found) != 1 {
		return "", errs.New(errs.UpstreamError, fmt.Sprintf("yt-dlp produced %d files", len(found)))
	}
	return found[0], nil
}

// classify maps yt-dlp stderr to an error code (docs/07-downloading.md).
func classify(stderr string) error {
	s := strings.ToLower(stderr)
	has := func(subs ...string) bool {
		for _, sub := range subs {
			if strings.Contains(s, sub) {
				return true
			}
		}
		return false
	}
	code := errs.UpstreamError
	switch {
	case has("login required", "rate-limit reached", "requested content is not available"):
		code = errs.LoginRequired
	case has("private video", "this video is private", "video unavailable", "has been removed", "http error 404"):
		code = errs.PrivateVideo
	case has("no video formats found", "there is no video in this post"):
		code = errs.UnsupportedContent
	case has("http error 429"):
		code = errs.RateLimited
	}
	return errs.New(code, lastLine(stderr))
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	if r := []rune(s); len(r) > stderrInMessage {
		s = string(r[:stderrInMessage])
	}
	return s
}

func isInstagram(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	h := strings.ToLower(u.Hostname())
	return h == "instagram.com" || strings.HasSuffix(h, ".instagram.com")
}

// limitedBuffer keeps the first max bytes and silently drops the rest, so a
// chatty process is never blocked or killed by a full pipe.
type limitedBuffer struct {
	bytes.Buffer
	max int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := b.max - b.Len(); room > 0 {
		if len(p) > room {
			b.Buffer.Write(p[:room])
		} else {
			b.Buffer.Write(p)
		}
	}
	return len(p), nil
}
