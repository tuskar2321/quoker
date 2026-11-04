// Package ytdlptest provides a scripted stand-in for the yt-dlp binary: a
// small program built once per test run. Use Main in TestMain and point the
// Runner at Install's result.
package ytdlptest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const envScenario = "QUOKER_FAKE_YTDLP_SCENARIO"

var fakeBin string

// Main builds the fake binary, runs the tests and removes the binary.
func Main(m *testing.M) int {
	dir, err := os.MkdirTemp("", "fake-ytdlp-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() { _ = os.RemoveAll(dir) }()
	fakeBin = filepath.Join(dir, "yt-dlp")
	build := exec.CommandContext(context.Background(), "go", "build", "-o", fakeBin, "github.com/tuskar2321/quoker/internal/worker/ytdlp/ytdlptest/fakeytdlp") //nolint:gosec // test helper, fixed arguments
	build.Stdout, build.Stderr = os.Stderr, os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "build fake yt-dlp:", err)
		return 1
	}
	return m.Run()
}

// Scenario scripts the fake.
type Scenario struct {
	InfoJSON           string         // printed for -J
	Stderr             string         // printed before failing with ExitCode
	ExitCode           int            // non-zero fails every call except --version
	LoginUnlessCookies bool           // fail with "login required" unless --cookies is passed
	FileBytes          map[string]int // download size by -f selector; default 1024
	Sleep              time.Duration  // delay before doing anything
	Log                string         // set by Install: one JSON args array per call
}

// Install writes the scenario where the re-executed binary finds it and
// returns the binary path and a function reading the recorded calls.
func Install(t *testing.T, sc Scenario) (bin string, calls func() [][]string) {
	t.Helper()
	dir := t.TempDir()
	sc.Log = filepath.Join(dir, "calls.jsonl")
	raw, err := json.Marshal(sc)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "scenario.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if fakeBin == "" {
		t.Fatal("ytdlptest.Main must run from TestMain")
	}
	t.Setenv(envScenario, path)
	return fakeBin, func() [][]string {
		b, _ := os.ReadFile(sc.Log)
		var out [][]string
		for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if line == "" {
				continue
			}
			var args []string
			_ = json.Unmarshal([]byte(line), &args)
			out = append(out, args)
		}
		return out
	}
}

// Run is the fake's entry point; it returns the process exit code.
func Run(args []string) int {
	path := os.Getenv(envScenario)
	raw, err := os.ReadFile(path) //nolint:gosec // path set by Install
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	var sc Scenario
	if err := json.Unmarshal(raw, &sc); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if slices.Contains(args, "--version") {
		fmt.Println("2026.10.01")
		return 0
	}
	logCall(sc.Log, args)
	time.Sleep(sc.Sleep)

	if sc.LoginUnlessCookies {
		cookies := value(args, "--cookies")
		if cookies == "" {
			fmt.Fprintln(os.Stderr, "ERROR: [Instagram] abc: login required")
			return 1
		}
		if st, err := os.Stat(cookies); err != nil || st.Mode().Perm() != 0o600 {
			fmt.Fprintln(os.Stderr, "ERROR: cookies file must be 0600")
			return 3
		}
	}
	if sc.ExitCode != 0 {
		fmt.Fprintln(os.Stderr, sc.Stderr)
		return sc.ExitCode
	}
	if slices.Contains(args, "-J") {
		fmt.Println(sc.InfoJSON)
		return 0
	}
	out := strings.ReplaceAll(value(args, "-o"), "%(id)s.%(ext)s", "video.mp4")
	size, ok := sc.FileBytes[value(args, "-f")]
	if !ok {
		size = 1024
	}
	if err := os.WriteFile(out, make([]byte, size), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	fmt.Println(out)
	return 0
}

func value(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func logCall(path string, args []string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // path set by Install
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	b, _ := json.Marshal(args)
	_, _ = f.Write(append(b, '\n'))
}
