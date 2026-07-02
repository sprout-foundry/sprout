package console

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// ToolInvocation is one row in the per-tool metrics breakdown that the
// CLI-D status-footer tooltip renders on Alt+T.
//
// The recorder is intentionally minimal — it tracks the same four
// numbers the TODO asks for (invocation count, total tokens, total
// cost, average latency) and is safe for concurrent use from any
// goroutine. Tools publish via RecordToolInvocation; the tooltip pulls
// via Snapshot / Sorted.
type ToolInvocation struct {
	Name         string
	Count        int64
	TotalTokens  int64
	TotalCost    int64 // store cents to avoid float drift; convert at render
	TotalLatency int64 // microseconds
}

// AvgLatency returns the average latency per invocation in milliseconds,
// rounded to two decimals. Zero if Count == 0.
func (t ToolInvocation) AvgLatency() float64 {
	if t.Count == 0 {
		return 0
	}
	return float64(t.TotalLatency) / float64(t.Count) / 1000.0
}

// MetricsRecorder aggregates per-tool invocation stats. Process-wide
// instance is exposed via GlobalMetricsRecorder; tests can build their
// own with NewMetricsRecorder for isolation.
type MetricsRecorder struct {
	mu       sync.RWMutex
	rows     map[string]*ToolInvocation
	started  time.Time
	totals   ToolInvocation // sum across all rows, denormalized for cheap reads
}

// NewMetricsRecorder constructs an empty recorder with started=now.
func NewMetricsRecorder() *MetricsRecorder {
	return &MetricsRecorder{
		rows:    make(map[string]*ToolInvocation),
		started: time.Now(),
	}
}

// RecordToolInvocation accumulates one observation. tokens and costUSD
// are per-invocation (not cumulative); the recorder adds them. latency
// is the per-invocation latency in microseconds.
func (m *MetricsRecorder) RecordToolInvocation(name string, tokens int64, costUSD float64, latencyMicros int64) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[name]
	if !ok {
		row = &ToolInvocation{Name: name}
		m.rows[name] = row
	}
	row.Count++
	row.TotalTokens += tokens
	row.TotalCost += int64(costUSD * 100) // store cents
	row.TotalLatency += latencyMicros
	m.totals.Count++
	m.totals.TotalTokens += tokens
	m.totals.TotalCost += int64(costUSD * 100)
	m.totals.TotalLatency += latencyMicros
}

// Snapshot returns a copy of all rows, sorted by Name. Safe for
// concurrent calls.
func (m *MetricsRecorder) Snapshot() []ToolInvocation {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ToolInvocation, 0, len(m.rows))
	for _, r := range m.rows {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Totals returns the aggregate row.
func (m *MetricsRecorder) Totals() ToolInvocation {
	if m == nil {
		return ToolInvocation{}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.totals
}

// StartedAt returns the time the recorder was created.
func (m *MetricsRecorder) StartedAt() time.Time {
	if m == nil {
		return time.Time{}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.started
}

// globalMetrics holds the process-wide recorder. Accessed via
// GlobalMetricsRecorder; tests can swap via SetGlobalMetricsRecorder.
var globalMetrics atomic.Pointer[MetricsRecorder]

func init() {
	globalMetrics.Store(NewMetricsRecorder())
}

// GlobalMetricsRecorder returns the process-wide recorder, creating one
// if init hasn't run yet (defensive — init above normally sets it).
func GlobalMetricsRecorder() *MetricsRecorder {
	r := globalMetrics.Load()
	if r == nil {
		r = NewMetricsRecorder()
		globalMetrics.Store(r)
	}
	return r
}

// SetGlobalMetricsRecorderForTest installs a recorder for the duration
// of a test. Returns a cleanup func that restores the previous recorder.
func SetGlobalMetricsRecorderForTest(r *MetricsRecorder) func() {
	prev := globalMetrics.Swap(r)
	return func() { globalMetrics.Store(prev) }
}