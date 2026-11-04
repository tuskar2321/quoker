// Package metrics exposes the core's counters in the Prometheus text format
// (docs/11-nfr-security.md, "Наблюдаемость"). The format is simple enough
// that the standard library suffices; no client library is pulled in.
package metrics

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Registry holds metrics in registration order.
type Registry struct {
	mu      sync.Mutex
	metrics []metric
}

type metric interface {
	write(ctx context.Context, w *strings.Builder) error
}

// New returns an empty Registry.
func New() *Registry { return &Registry{} }

func (r *Registry) add(m metric) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.metrics = append(r.metrics, m)
}

// Counter is a monotonically increasing value per label set.
type Counter struct {
	name, help string
	labels     []string
	mu         sync.Mutex
	values     map[string]float64 // joined label values → value
}

// Counter registers a counter with the given label names.
func (r *Registry) Counter(name, help string, labels ...string) *Counter {
	c := &Counter{name: name, help: help, labels: labels, values: map[string]float64{}}
	r.add(c)
	return c
}

// Inc adds one for the label values (in registration order).
func (c *Counter) Inc(values ...string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values[key(values)]++
}

func (c *Counter) write(_ context.Context, w *strings.Builder) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	header(w, c.name, c.help, "counter")
	for _, k := range sortedKeys(c.values) {
		_, _ = fmt.Fprintf(w, "%s%s %s\n", c.name, labelSet(c.labels, split(k)), num(c.values[k]))
	}
	return nil
}

// Histogram counts observations into cumulative buckets per label set.
type Histogram struct {
	name, help string
	labels     []string
	buckets    []float64
	mu         sync.Mutex
	series     map[string]*hseries
}

type hseries struct {
	counts []uint64 // per bucket, not cumulative
	count  uint64
	sum    float64
}

// Histogram registers a histogram; buckets are upper bounds in ascending order.
func (r *Registry) Histogram(name, help string, buckets []float64, labels ...string) *Histogram {
	h := &Histogram{name: name, help: help, labels: labels, buckets: buckets, series: map[string]*hseries{}}
	r.add(h)
	return h
}

// Observe records v for the label values.
func (h *Histogram) Observe(v float64, values ...string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	k := key(values)
	s, ok := h.series[k]
	if !ok {
		s = &hseries{counts: make([]uint64, len(h.buckets))}
		h.series[k] = s
	}
	for i, b := range h.buckets {
		if v <= b {
			s.counts[i]++
			break
		}
	}
	s.count++
	s.sum += v
}

func (h *Histogram) write(_ context.Context, w *strings.Builder) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	header(w, h.name, h.help, "histogram")
	keys := make([]string, 0, len(h.series))
	for k := range h.series {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		s, vals := h.series[k], split(k)
		withLe := slices.Concat(h.labels, []string{"le"})
		var cum uint64
		for i, b := range h.buckets {
			cum += s.counts[i]
			_, _ = fmt.Fprintf(w, "%s_bucket%s %d\n", h.name, labelSet(withLe, slices.Concat(vals, []string{num(b)})), cum)
		}
		_, _ = fmt.Fprintf(w, "%s_bucket%s %d\n", h.name, labelSet(withLe, slices.Concat(vals, []string{"+Inf"})), s.count)
		_, _ = fmt.Fprintf(w, "%s_sum%s %s\n", h.name, labelSet(h.labels, vals), num(s.sum))
		_, _ = fmt.Fprintf(w, "%s_count%s %d\n", h.name, labelSet(h.labels, vals), s.count)
	}
	return nil
}

// Sample is one gauge value read at scrape time.
type Sample struct {
	Values []string // label values in registration order
	Value  float64
}

type gaugeFunc struct {
	name, help string
	labels     []string
	read       func(ctx context.Context) ([]Sample, error)
}

// GaugeFunc registers a gauge whose samples are read on every scrape, e.g.
// queue sizes straight from the database.
func (r *Registry) GaugeFunc(name, help string, read func(ctx context.Context) ([]Sample, error), labels ...string) {
	r.add(&gaugeFunc{name: name, help: help, labels: labels, read: read})
}

func (g *gaugeFunc) write(ctx context.Context, w *strings.Builder) error {
	samples, err := g.read(ctx)
	if err != nil {
		return fmt.Errorf("%s: %w", g.name, err)
	}
	header(w, g.name, g.help, "gauge")
	for _, s := range samples {
		_, _ = fmt.Fprintf(w, "%s%s %s\n", g.name, labelSet(g.labels, s.Values), num(s.Value))
	}
	return nil
}

// Handler serves the metrics. A gauge that fails to read is skipped and
// logged, the rest are still served.
func (r *Registry) Handler(log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx, cancel := context.WithTimeout(req.Context(), 5*time.Second)
		defer cancel()
		r.mu.Lock()
		ms := append([]metric(nil), r.metrics...)
		r.mu.Unlock()
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		var b strings.Builder
		for _, m := range ms {
			var part strings.Builder
			if err := m.write(ctx, &part); err != nil {
				log.Warn("metric not collected", "err", err)
				continue
			}
			b.WriteString(part.String())
		}
		_, _ = io.WriteString(w, b.String())
	})
}

// header and the write methods append to an in-memory builder, which
// cannot fail.
func header(w *strings.Builder, name, help, kind string) {
	_, _ = fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, kind)
}

func labelSet(names, values []string) string {
	if len(names) == 0 {
		return ""
	}
	parts := make([]string, len(names))
	for i, n := range names {
		v := ""
		if i < len(values) {
			v = values[i]
		}
		parts[i] = n + `="` + escape(v) + `"`
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func escape(v string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(v)
}

func num(v float64) string {
	if math.IsInf(v, 1) {
		return "+Inf"
	}
	return strconv.FormatFloat(v, 'f', -1, 64) // no exponent: byte counts stay readable
}

// Label values are joined with a byte that cannot appear in them.
const sep = "\x00"

func key(values []string) string { return strings.Join(values, sep) }

func split(k string) []string {
	if k == "" {
		return nil
	}
	return strings.Split(k, sep)
}

func sortedKeys(m map[string]float64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
