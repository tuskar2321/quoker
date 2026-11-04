package runner

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	linkv1 "github.com/tuskar2321/quoker/gen/quoker/link/v1"
)

// --- fakes ---

type fakeConn struct {
	mu      sync.Mutex
	results []*linkv1.TaskResult
	progr   []int32
}

func (c *fakeConn) SendResult(r *linkv1.TaskResult) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.results = append(c.results, r)
	return nil
}

func (c *fakeConn) SendProgress(_ string, pct int32) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.progr = append(c.progr, pct)
	return nil
}

func (c *fakeConn) lastResult() *linkv1.TaskResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.results) == 0 {
		return nil
	}
	return c.results[len(c.results)-1]
}

type fakeConfig struct{ cfg *linkv1.Config }

func (f *fakeConfig) CoreConfig() *linkv1.Config { return f.cfg }

type fakeResolve struct{ err error }

func (f *fakeResolve) Resolve(_ context.Context, _ *linkv1.ResolveTask, _ *linkv1.Config, prog Progress) (*linkv1.ResolveResult, error) {
	prog(50)
	prog(100)
	if f.err != nil {
		return nil, f.err
	}
	return &linkv1.ResolveResult{Platform: "tiktok", NativeId: "42"}, nil
}

type resolveFunc func(ctx context.Context) (*linkv1.ResolveResult, error)

func (f resolveFunc) Resolve(ctx context.Context, _ *linkv1.ResolveTask, _ *linkv1.Config, _ Progress) (*linkv1.ResolveResult, error) {
	return f(ctx)
}

type fakeBotCall struct{}

func (f *fakeBotCall) BotCall(_ context.Context, _ *linkv1.BotCallTask, _ *linkv1.Config) (*linkv1.BotCallResult, error) {
	return &linkv1.BotCallResult{ResponseJson: `{"ok":true}`}, nil
}

type panicResolve struct{}

func (p *panicResolve) Resolve(_ context.Context, _ *linkv1.ResolveTask, _ *linkv1.Config, _ Progress) (*linkv1.ResolveResult, error) {
	panic("test panic")
}

// --- helpers ---

func defaultCfg() *linkv1.Config {
	return &linkv1.Config{BotToken: "tok", MaxParallel: 2}
}

func makeRunner(h Handlers) (*Runner, *fakeConfig, *fakeConn) {
	cfg := &fakeConfig{cfg: defaultCfg()}
	conn := &fakeConn{}
	r := New(h, cfg, 2, nil) // nil logger → uses slog.Default
	return r, cfg, conn
}

func resolveTask() *linkv1.Task {
	return &linkv1.Task{
		TaskId:  "t1",
		LeaseMs: 60_000,
		Kind:    &linkv1.Task_Resolve{Resolve: &linkv1.ResolveTask{Url: "https://www.tiktok.com/@a/video/42"}},
	}
}

// --- tests ---

func TestHandle_success(t *testing.T) {
	r, _, conn := makeRunner(Handlers{Resolve: &fakeResolve{}})
	if err := r.Handle(context.Background(), resolveTask(), conn); err != nil {
		t.Fatal(err)
	}
	res := conn.lastResult()
	if res == nil || res.GetResolve() == nil {
		t.Fatalf("expected ResolveResult, got %+v", res)
	}
	if res.GetResolve().NativeId != "42" {
		t.Fatalf("native_id = %s", res.GetResolve().NativeId)
	}
	// Progress was sent.
	if len(conn.progr) != 2 || conn.progr[0] != 50 || conn.progr[1] != 100 {
		t.Fatalf("progress = %v", conn.progr)
	}
}

func TestHandle_taskError(t *testing.T) {
	te := NewError("private_video", "video is private", false, nil)
	r, _, conn := makeRunner(Handlers{Resolve: &fakeResolve{err: te}})
	if err := r.Handle(context.Background(), resolveTask(), conn); err != nil {
		t.Fatal(err)
	}
	res := conn.lastResult()
	if e := res.GetError(); e == nil || e.Code != "private_video" || e.Retryable {
		t.Fatalf("unexpected error result: %+v", res)
	}
}

func TestHandle_upstreamError_retryable(t *testing.T) {
	te := NewError("upstream_error", "timeout", true, errors.New("net timeout"))
	r, _, conn := makeRunner(Handlers{Resolve: &fakeResolve{err: te}})
	_ = r.Handle(context.Background(), resolveTask(), conn)
	if e := conn.lastResult().GetError(); e == nil || !e.Retryable {
		t.Fatalf("expected retryable error")
	}
}

func TestHandle_contextCancelled(t *testing.T) {
	r, _, conn := makeRunner(Handlers{Resolve: &fakeResolve{err: context.Canceled}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = r.Handle(ctx, resolveTask(), conn)
	if e := conn.lastResult().GetError(); e == nil || e.Code != "cancelled" {
		t.Fatalf("expected cancelled error, got %+v", conn.lastResult())
	}
}

func TestHandle_panic(t *testing.T) {
	r, _, conn := makeRunner(Handlers{Resolve: &panicResolve{}})
	if err := r.Handle(context.Background(), resolveTask(), conn); err != nil {
		t.Fatal(err)
	}
	if e := conn.lastResult().GetError(); e == nil || e.Code != "internal" {
		t.Fatalf("expected internal error from panic, got %+v", conn.lastResult())
	}
}

func TestHandle_unknownKind(t *testing.T) {
	r, _, conn := makeRunner(Handlers{})
	task := &linkv1.Task{TaskId: "t2", LeaseMs: 60_000} // nil Kind
	_ = r.Handle(context.Background(), task, conn)
	if e := conn.lastResult().GetError(); e == nil || e.Code != "internal" {
		t.Fatalf("expected internal error for nil kind, got %+v", conn.lastResult())
	}
}

func TestHandle_noConfig(t *testing.T) {
	cfg := &fakeConfig{cfg: nil}
	conn := &fakeConn{}
	r := New(Handlers{Resolve: &fakeResolve{}}, cfg, 2, nil)
	_ = r.Handle(context.Background(), resolveTask(), conn)
	if e := conn.lastResult().GetError(); e == nil || e.Code != "internal" {
		t.Fatalf("expected internal error when config is nil, got %+v", conn.lastResult())
	}
}

func TestHandle_botCall(t *testing.T) {
	r, _, conn := makeRunner(Handlers{BotCall: &fakeBotCall{}})
	task := &linkv1.Task{
		TaskId:  "t3",
		LeaseMs: 30_000,
		Kind:    &linkv1.Task_BotCall{BotCall: &linkv1.BotCallTask{Method: "getMe"}},
	}
	_ = r.Handle(context.Background(), task, conn)
	if res := conn.lastResult().GetBotCall(); res == nil || res.ResponseJson == "" {
		t.Fatalf("expected BotCallResult, got %+v", conn.lastResult())
	}
}

func TestHandle_missingHandler(t *testing.T) {
	// No Resolve handler configured.
	r, _, conn := makeRunner(Handlers{})
	_ = r.Handle(context.Background(), resolveTask(), conn)
	if e := conn.lastResult().GetError(); e == nil || e.Code != "internal" {
		t.Fatalf("expected internal error for missing handler")
	}
}

func TestTaskDeadline(t *testing.T) {
	tests := []struct {
		name string
		task *linkv1.Task
		want time.Duration
	}{
		{"resolve: lease minus margin", &linkv1.Task{LeaseMs: 60_000, Kind: &linkv1.Task_Resolve{Resolve: &linkv1.ResolveTask{}}}, 55 * time.Second},
		{"bot call", &linkv1.Task{LeaseMs: 30_000, Kind: &linkv1.Task_BotCall{BotCall: &linkv1.BotCallTask{}}}, 25 * time.Second},
		{"send video: hard cap", &linkv1.Task{LeaseMs: 600_000, Kind: &linkv1.Task_SendVideo{SendVideo: &linkv1.SendVideoTask{}}}, 30 * time.Minute},
		{"no lease", &linkv1.Task{Kind: &linkv1.Task_Resolve{Resolve: &linkv1.ResolveTask{}}}, time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := taskDeadline(tt.task); got != tt.want {
				t.Fatalf("deadline = %s, want %s", got, tt.want)
			}
		})
	}
}

// TestHandle_appliesDeadline: a handler that never returns on its own is
// stopped by the task deadline (REVIEW H8).
func TestHandle_appliesDeadline(t *testing.T) {
	var deadline time.Time
	r, _, conn := makeRunner(Handlers{Resolve: resolveFunc(func(ctx context.Context) (*linkv1.ResolveResult, error) {
		deadline, _ = ctx.Deadline()
		return &linkv1.ResolveResult{}, nil
	})})
	task := resolveTask()
	task.LeaseMs = 60_000
	_ = r.Handle(context.Background(), task, conn)
	if left := time.Until(deadline); left < 50*time.Second || left > 56*time.Second {
		t.Fatalf("deadline in %s, want about 55 s", left)
	}
}
