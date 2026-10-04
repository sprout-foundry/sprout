// langguard_metrics.go — the per-model language-guard metric (SP-152 §152e,
// item 152.9). Every final message the guard judges is counted as a check;
// a reliable language mismatch is counted as a mismatch. The per-model
// mismatch rate (mismatches / checks) is the diagnostic signal: a high rate
// for a given model means that model frequently replies in the wrong
// language. Role is not tracked yet (the role dimension is added in 150.5);
// the metric is keyed by model ID only.
package agent

import (
	"sort"
	"sync"
	"sync/atomic"
)

// LanguageGuardModelStat is one model's language-guard metric: how many final
// messages the guard judged for that model (Checks) and how many of those
// were a reliable mismatch (Mismatches).
type LanguageGuardModelStat struct {
	ModelID    string
	Checks     int64
	Mismatches int64
}

// Rate returns this model's mismatch rate: mismatches / checks. It is 0 when
// no checks have been recorded (a model never judged has no measurable rate).
func (s LanguageGuardModelStat) Rate() float64 {
	if s.Checks == 0 {
		return 0
	}
	return float64(s.Mismatches) / float64(s.Checks)
}

// LanguageGuardMetrics aggregates per-model language-guard observations into
// a per-model mismatch rate (SP-152 §152e). It is process-wide and safe for
// concurrent use from any goroutine (the guard runs on the agent's query
// path, which can be concurrent across chats). The process-wide instance is
// exposed via GlobalLanguageGuardMetrics; tests build their own with
// NewLanguageGuardMetrics for isolation.
type LanguageGuardMetrics struct {
	mu     sync.RWMutex
	models map[string]*LanguageGuardModelStat
}

// NewLanguageGuardMetrics constructs an empty recorder.
func NewLanguageGuardMetrics() *LanguageGuardMetrics {
	return &LanguageGuardMetrics{models: make(map[string]*LanguageGuardModelStat)}
}

// Record counts one guarded judgment of a final message for a model. A check
// is always counted; a mismatch is counted additionally when mismatched is
// true. This keeps the invariant Mismatches <= Checks per model. A nil
// receiver is a no-op. An empty model ID is bucketed under "unknown" so a
// missing model never drops the observation.
func (m *LanguageGuardMetrics) Record(modelID string, mismatched bool) {
	if m == nil {
		return
	}
	if modelID == "" {
		modelID = "unknown"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	stat, ok := m.models[modelID]
	if !ok {
		stat = &LanguageGuardModelStat{ModelID: modelID}
		m.models[modelID] = stat
	}
	stat.Checks++
	if mismatched {
		stat.Mismatches++
	}
}

// Snapshot returns a copy of every per-model stat, sorted by ModelID for
// stable output. A nil receiver returns nil.
func (m *LanguageGuardMetrics) Snapshot() []LanguageGuardModelStat {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]LanguageGuardModelStat, 0, len(m.models))
	for _, s := range m.models {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModelID < out[j].ModelID })
	return out
}

// OverallRate returns the aggregate mismatch rate across all models (total
// mismatches / total checks; 0 when no checks have been recorded).
func (m *LanguageGuardMetrics) OverallRate() float64 {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var checks, mismatches int64
	for _, s := range m.models {
		checks += s.Checks
		mismatches += s.Mismatches
	}
	if checks == 0 {
		return 0
	}
	return float64(mismatches) / float64(checks)
}

// globalLangGuardMetrics holds the process-wide recorder. Accessed via
// GlobalLanguageGuardMetrics; tests can swap via SetGlobalLanguageGuardMetricsForTest.
var globalLangGuardMetrics atomic.Pointer[LanguageGuardMetrics]

func init() {
	globalLangGuardMetrics.Store(NewLanguageGuardMetrics())
}

// GlobalLanguageGuardMetrics returns the process-wide recorder, creating one
// if init hasn't run yet (defensive — init above normally sets it).
func GlobalLanguageGuardMetrics() *LanguageGuardMetrics {
	r := globalLangGuardMetrics.Load()
	if r == nil {
		r = NewLanguageGuardMetrics()
		globalLangGuardMetrics.Store(r)
	}
	return r
}

// SetGlobalLanguageGuardMetricsForTest installs a recorder for the duration
// of a test. It returns a cleanup func that restores the previous recorder.
func SetGlobalLanguageGuardMetricsForTest(r *LanguageGuardMetrics) func() {
	prev := globalLangGuardMetrics.Swap(r)
	return func() { globalLangGuardMetrics.Store(prev) }
}

// LanguageGuardStats is the Agent's diagnostic accessor for the language-guard
// metric (SP-152 §152e): it returns the process-wide per-model snapshot — for
// each model, how many final messages the guard judged and how many were
// mismatches, with a mismatch rate per model. A high rate for a model means
// it frequently replies in the wrong language. It is pure Go (no cgo), so it
// works in the native daemon, the CLI, and the WASM build alike.
func (a *Agent) LanguageGuardStats() []LanguageGuardModelStat {
	return GlobalLanguageGuardMetrics().Snapshot()
}
