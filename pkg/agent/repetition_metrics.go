// repetition_metrics.go — the per-(provider, model) streamed-repetition
// metric. Every time the repetition guard cuts a degenerate run it records one
// loop for that (provider, model) pair, so a model that degenerates frequently
// is visible in the metrics. The counter is process-wide and safe for
// concurrent use (the guard runs on the agent's streaming path, which can be
// concurrent across chats); it mirrors the language-guard metric's shape.
package agent

import (
	"sort"
	"sync"
	"sync/atomic"
)

// RepetitionLoopStat is one (provider, model) cell of the repetition metric:
// how many degenerate repetition loops the guard cut for that pair.
type RepetitionLoopStat struct {
	Provider string
	Model    string
	Loops    int64
	// ReasoningLoops counts the subset detected in the reasoning channel
	// rather than the visible reply. Those are steered, not cut, so they
	// are reported separately: a model that thinks in circles while still
	// answering correctly shows up here and nowhere else.
	ReasoningLoops int64
	// RecoveryEscalations counts how often the nudge retry also looped and
	// the recovery-compaction escalation ran (de-loop + optional LLM
	// summary + final attempt). A model whose RecoveryEscalations are
	// nearly equal to its Loops is one where the nudge never suffices —
	// the compaction is the thing that saves those turns.
	RecoveryEscalations int64
	// RecoveryRecoveries counts how often the escalation's final attempt
	// produced a non-loop reply: the escalation fired AND the model
	// recovered. Escalations that looped a third time (terminal failure)
	// are not counted here, so Recoveries ≤ Escalations.
	RecoveryRecoveries int64
}

// repetitionKey is the (provider, model) bucket of the metric.
type repetitionKey struct {
	provider string
	model    string
}

// RepetitionMetrics aggregates per-(provider, model) repetition-loop counts.
// The process-wide instance is exposed via GlobalRepetitionMetrics; tests
// build their own with NewRepetitionMetrics for isolation.
type RepetitionMetrics struct {
	mu    sync.RWMutex
	stats map[repetitionKey]*RepetitionLoopStat
}

// NewRepetitionMetrics constructs an empty recorder.
func NewRepetitionMetrics() *RepetitionMetrics {
	return &RepetitionMetrics{stats: make(map[repetitionKey]*RepetitionLoopStat)}
}

// Record counts one cut repetition loop for a (provider, model) pair. A nil
// receiver is a no-op. An empty provider or model is bucketed under "unknown"
// so a missing value never drops the observation.
func (m *RepetitionMetrics) Record(provider, model string) {
	m.record(provider, model, false)
}

// RecordReasoning counts one degenerate loop detected in the reasoning
// channel. It also increments the cell's total, so Loops stays the count of
// every loop the guard caught and ReasoningLoops the reasoning subset.
func (m *RepetitionMetrics) RecordReasoning(provider, model string) {
	m.record(provider, model, true)
}

// RecordRecoveryEscalation counts one recovery-compaction escalation: the
// nudge retry also looped, so the provider ran the de-loop + (optional) LLM
// summary and a final attempt. It is the denominator the recovery counters
// are read against — a model whose RecoveryEscalations track its Loops is
// one where the nudge alone never suffices.
func (m *RepetitionMetrics) RecordRecoveryEscalation(provider, model string) {
	m.incrRecovery(provider, model, func(s *RepetitionLoopStat) { s.RecoveryEscalations++ })
}

// RecordRecoverySuccess counts one escalation whose final attempt produced a
// non-loop reply: the recovery compaction saved the turn. An escalation whose
// final attempt loops a third time (terminal failure) is NOT counted, so
// RecoveryRecoveries ≤ RecoveryEscalations always holds.
func (m *RepetitionMetrics) RecordRecoverySuccess(provider, model string) {
	m.incrRecovery(provider, model, fnRecoverySuccess)
}

// incrRecovery normalizes the key, locks the cell, and applies one of the
// recovery counters — keeping the lookup logic in one place.
func (m *RepetitionMetrics) incrRecovery(provider, model string, apply func(*RepetitionLoopStat)) {
	if m == nil {
		return
	}
	if provider == "" {
		provider = "unknown"
	}
	if model == "" {
		model = "unknown"
	}
	key := repetitionKey{provider: provider, model: model}
	m.mu.Lock()
	defer m.mu.Unlock()
	stat, ok := m.stats[key]
	if !ok {
		stat = &RepetitionLoopStat{Provider: provider, Model: model}
		m.stats[key] = stat
	}
	apply(stat)
}

func fnRecoverySuccess(s *RepetitionLoopStat) { s.RecoveryRecoveries++ }

func (m *RepetitionMetrics) record(provider, model string, reasoning bool) {
	if m == nil {
		return
	}
	if provider == "" {
		provider = "unknown"
	}
	if model == "" {
		model = "unknown"
	}
	key := repetitionKey{provider: provider, model: model}
	m.mu.Lock()
	defer m.mu.Unlock()
	stat, ok := m.stats[key]
	if !ok {
		stat = &RepetitionLoopStat{Provider: provider, Model: model}
		m.stats[key] = stat
	}
	stat.Loops++
	if reasoning {
		stat.ReasoningLoops++
	}
}

// Snapshot returns a copy of every per-(provider, model) stat, sorted by
// Provider then Model for stable output. A nil receiver returns nil.
func (m *RepetitionMetrics) Snapshot() []RepetitionLoopStat {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]RepetitionLoopStat, 0, len(m.stats))
	for _, s := range m.stats {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Provider != out[j].Provider {
			return out[i].Provider < out[j].Provider
		}
		return out[i].Model < out[j].Model
	})
	return out
}

// Total returns the aggregate loop count across every (provider, model) cell.
func (m *RepetitionMetrics) Total() int64 {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var total int64
	for _, s := range m.stats {
		total += s.Loops
	}
	return total
}

// globalRepetitionMetrics holds the process-wide recorder. Accessed via
// GlobalRepetitionMetrics; tests can swap via SetGlobalRepetitionMetricsForTest.
var globalRepetitionMetrics atomic.Pointer[RepetitionMetrics]

func init() {
	globalRepetitionMetrics.Store(NewRepetitionMetrics())
}

// GlobalRepetitionMetrics returns the process-wide recorder, creating one if
// init hasn't run yet (defensive — init above normally sets it).
func GlobalRepetitionMetrics() *RepetitionMetrics {
	r := globalRepetitionMetrics.Load()
	if r == nil {
		r = NewRepetitionMetrics()
		globalRepetitionMetrics.Store(r)
	}
	return r
}

// SetGlobalRepetitionMetricsForTest installs a recorder for the duration of a
// test. It returns a cleanup func that restores the previous recorder.
func SetGlobalRepetitionMetricsForTest(r *RepetitionMetrics) func() {
	prev := globalRepetitionMetrics.Swap(r)
	return func() { globalRepetitionMetrics.Store(prev) }
}

// RepetitionLoopStats is the Agent's diagnostic accessor for the repetition
// metric: it returns the process-wide per-(provider, model) snapshot — how
// many degenerate repetition loops the guard cut for each pair. It is pure Go
// (no cgo), so it works in the native daemon, the CLI, and the WASM build
// alike.
func (a *Agent) RepetitionLoopStats() []RepetitionLoopStat {
	return GlobalRepetitionMetrics().Snapshot()
}
