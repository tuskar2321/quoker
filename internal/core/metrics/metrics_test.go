package metrics_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tuskar2321/quoker/internal/core/metrics"
)

func TestExposition(t *testing.T) {
	r := metrics.New()
	c := r.Counter("quoker_http_responses_total", "HTTP responses by status code.", "code")
	c.Inc("200")
	c.Inc("200")
	c.Inc("404")
	h := r.Histogram("quoker_task_duration_seconds", "Task duration.", []float64{1, 10}, "kind", "platform")
	h.Observe(0.5, "resolve", "tiktok")
	h.Observe(5, "resolve", "tiktok")
	h.Observe(50, "resolve", "tiktok")
	r.GaugeFunc("quoker_queue_tasks", "Tasks by status.", func(context.Context) ([]metrics.Sample, error) {
		return []metrics.Sample{{Values: []string{"queued"}, Value: 3}}, nil
	}, "status")
	r.GaugeFunc("quoker_broken", "Fails to read.", func(context.Context) ([]metrics.Sample, error) {
		return nil, errors.New("db down")
	})
	c2 := r.Counter("quoker_odd_labels_total", "Escaping.", "v")
	c2.Inc(`a"b\c`)

	rec := httptest.NewRecorder()
	r.Handler(slog.New(slog.NewTextHandler(io.Discard, nil))).ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil)) //nolint:noctx // test
	body := rec.Body.String()
	for _, want := range []string{
		"# TYPE quoker_http_responses_total counter",
		`quoker_http_responses_total{code="200"} 2`,
		`quoker_http_responses_total{code="404"} 1`,
		`quoker_task_duration_seconds_bucket{kind="resolve",platform="tiktok",le="1"} 1`,
		`quoker_task_duration_seconds_bucket{kind="resolve",platform="tiktok",le="10"} 2`,
		`quoker_task_duration_seconds_bucket{kind="resolve",platform="tiktok",le="+Inf"} 3`,
		`quoker_task_duration_seconds_sum{kind="resolve",platform="tiktok"} 55.5`,
		`quoker_task_duration_seconds_count{kind="resolve",platform="tiktok"} 3`,
		`quoker_queue_tasks{status="queued"} 3`,
		`quoker_odd_labels_total{v="a\"b\\c"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
	if strings.Contains(body, "quoker_broken") {
		t.Error("a failing gauge must be skipped")
	}
}
