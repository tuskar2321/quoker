package results_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"
	"time"

	linkv1 "github.com/tuskar2321/quoker/gen/quoker/link/v1"
	"github.com/tuskar2321/quoker/internal/core/files"
	"github.com/tuskar2321/quoker/internal/core/queue"
	"github.com/tuskar2321/quoker/internal/core/results"
	"github.com/tuskar2321/quoker/internal/core/sends"
	"github.com/tuskar2321/quoker/internal/core/store"
	"github.com/tuskar2321/quoker/internal/core/targets"
	"github.com/tuskar2321/quoker/internal/core/videos"
)

type env struct {
	st    *store.Store
	q     *queue.Store
	vids  *videos.Service
	tgts  *targets.Service
	snds  *sends.Service
	files *files.Store
	h     *results.Handler
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "q.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	q := queue.New(st, nil)
	v := videos.New(st, q, nil)
	tg := targets.New(st, nil)
	s := sends.New(st, q, v, tg, nil)
	f, err := files.New(st, t.TempDir(), 1<<30, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return &env{st: st, q: q, vids: v, tgts: tg, snds: s, files: f, h: results.New(v, s, f)}
}

func TestTaskSucceeded_resolve(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	id, err := e.q.Enqueue(ctx, queue.EnqueueParams{
		Kind: queue.KindResolve, Payload: []byte{}, DedupKey: "resolve:tiktok:short:ZM1",
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err := e.q.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	err = e.h.TaskSucceeded(ctx, task, &linkv1.TaskResult{
		TaskId: id,
		Result: &linkv1.TaskResult_Resolve{Resolve: &linkv1.ResolveResult{
			Platform: "tiktok", NativeId: "42", CanonicalUrl: "https://www.tiktok.com/@a/video/42",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.vids.Get(ctx, "tiktok:42"); err != nil {
		t.Fatalf("video not stored: %v", err)
	}
	var alias string
	if err := e.st.DB().QueryRowContext(ctx,
		`SELECT video_id FROM video_aliases WHERE url_normalized = 'tiktok:short:ZM1'`).Scan(&alias); err != nil {
		t.Fatalf("alias not stored: %v", err)
	}
	if alias != "tiktok:42" {
		t.Fatalf("alias -> %q", alias)
	}
}

func TestSendLifecycle(t *testing.T) {
	tests := []struct {
		name    string
		succeed bool
		want    sends.Status
	}{
		{"sent", true, sends.StatusSent},
		{"failed", false, sends.StatusFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			ctx := context.Background()
			now := time.Now().UnixMilli()
			res, err := e.st.DB().ExecContext(ctx,
				`INSERT INTO users (role, status, tg_user_id, created_at) VALUES ('user','active',1,?)`, now)
			if err != nil {
				t.Fatal(err)
			}
			uid, _ := res.LastInsertId()
			if _, err := e.st.DB().ExecContext(ctx,
				`INSERT INTO videos (id, platform, native_id, canonical_url, file_status, resolved_at, expires_at, last_access_at)
				 VALUES ('tiktok:1','tiktok','1','https://x','none',?,?,?)`, now, now+86400000, now); err != nil {
				t.Fatal(err)
			}
			tgt, err := e.tgts.Upsert(ctx, targets.Target{
				OwnerUserID: uid, ChatID: -1, Type: "channel", Title: "C", Status: targets.StatusActive,
			})
			if err != nil {
				t.Fatal(err)
			}
			snd, err := e.snds.Create(ctx, sends.CreateParams{
				VideoID: "tiktok:1", TargetID: tgt.ID, IdempotencyKey: "k", CallerID: uid,
			})
			if err != nil {
				t.Fatal(err)
			}
			task, err := e.q.Get(ctx, snd.TaskID)
			if err != nil {
				t.Fatal(err)
			}
			if tt.succeed {
				err = e.h.TaskSucceeded(ctx, task, &linkv1.TaskResult{
					TaskId: task.ID,
					Result: &linkv1.TaskResult_SendVideo{SendVideo: &linkv1.SendVideoResult{MessageId: 5}},
				})
			} else {
				task.ErrorCode = "target_inactive"
				err = e.h.TaskFailed(ctx, task)
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := e.snds.Get(ctx, uid, snd.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != tt.want {
				t.Fatalf("status = %q, want %q", got.Status, tt.want)
			}
		})
	}
}

func TestDownloadResult(t *testing.T) {
	data := []byte("video bytes")
	sum := sha256.Sum256(data)
	good := hex.EncodeToString(sum[:])
	tests := []struct {
		name     string
		sha      string
		want     string
		wantFile bool
	}{
		{"verified", good, "ready", true},
		{"corrupted", strings.Repeat("0", 64), "failed", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			ctx := context.Background()
			now := time.Now().UnixMilli()
			if _, err := e.st.DB().ExecContext(ctx,
				`INSERT INTO videos (id, platform, native_id, canonical_url, file_status, resolved_at, expires_at, last_access_at)
				 VALUES ('tiktok:1','tiktok','1','https://x','none',?,?,?)`, now, now+86400000, now); err != nil {
				t.Fatal(err)
			}
			v, err := e.vids.Get(ctx, "tiktok:1")
			if err != nil {
				t.Fatal(err)
			}
			taskID, err := e.vids.EnqueueDownload(ctx, v, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := e.files.Append("tiktok:1", 0, int64(len(data)), bytes.NewReader(data)); err != nil {
				t.Fatal(err)
			}
			task, _ := e.q.Get(ctx, taskID)
			err = e.h.TaskSucceeded(ctx, task, &linkv1.TaskResult{TaskId: taskID,
				Result: &linkv1.TaskResult_Download{Download: &linkv1.DownloadResult{SizeBytes: int64(len(data)), Sha256: tt.sha}}})
			if (err == nil) != tt.wantFile {
				t.Fatalf("err = %v", err)
			}
			got, _ := e.vids.Get(ctx, "tiktok:1")
			if string(got.FileStatus) != tt.want || e.files.HasFile("tiktok:1") != tt.wantFile {
				t.Fatalf("file_status = %q, on disk = %v", got.FileStatus, e.files.HasFile("tiktok:1"))
			}
		})
	}
}
