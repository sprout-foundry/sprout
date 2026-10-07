// langguard_metrics.go — the per-model language-guard metric.
// Every final message the guard judges is counted as a check;
// a reliable language mismatch is counted as a mismatch. The per-model
// mismatch rate (mismatches / checks) is the diagnostic signal: a high rate
// for a given model means that model frequently replies in the wrong
// language. The role dimension buckets each
// check under the agent's role, so the metric keys by (model, role): an
// empty model or role is bucketed under "unknown".
package agent

import (
	"sort"
	"sync"
	"sync/atomic"
)

// LanguageGuardModelStat is one (model, role) cell of the language-guard
// metric: how many final messages the guard judged for that model under that
// role (Checks) and how many of those were a reliable mismatch (Mismatches).
type LanguageGuardModelStat struct {
	ModelID    string
	Role       string
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

// langGuardKey is the (model, role) bucket of the language-guard metric.
type langGuardKey struct {
	model string
	role  string
}

// LanguageGuardMetrics aggregates per-(model, role) language-guard
// observations into a per-(model, role) mismatch rate.
// It is process-wide and safe for concurrent use from any goroutine
// (the guard runs on the agent's query path, which can be concurrent across
// chats). The process-wide instance is exposed via
// GlobalLanguageGuardMetrics; tests build their own with
// NewLanguageGuardMetrics for isolation.
type LanguageGuardMetrics struct {
	mu    sync.RWMutex
	stats map[langGuardKey]*LanguageGuardModelStat
}

// NewLanguageGuardMetrics constructs an empty recorder.
func NewLanguageGuardMetrics() *LanguageGuardMetrics {
	return &LanguageGuardMetrics{stats: make(map[langGuardKey]*LanguageGuardModelStat)}
}

// Record counts one guarded judgment of a final message for a (model, role)
// pair. A check is always counted; a mismatch is counted additionally when
// mismatched is true. This keeps the invariant Mismatches <= Checks per
// (model, role). A nil receiver is a no-op. An empty model ID or role is
// bucketed under "unknown" so a missing value never drops the observation.
func (m *LanguageGuardMetrics) Record(modelID, role string, mismatched bool) {
	if m == nil {
		return
	}
	if modelID == "" {
		modelID = "unknown"
	}
	if role == "" {
		role = "unknown"
	}
	key := langGuardKey{model: modelID, role: role}
	m.mu.Lock()
	defer m.mu.Unlock()
	stat, ok := m.stats[key]
	if !ok {
		stat = &LanguageGuardModelStat{ModelID: modelID, Role: role}
		m.stats[key] = stat
	}
	stat.Checks++
	if mismatched {
		stat.Mismatches++
	}
}

// Snapshot returns a copy of every per-(model, role) stat, sorted by
// ModelID then Role for stable output. A nil receiver returns nil.
func (m *LanguageGuardMetrics) Snapshot() []LanguageGuardModelStat {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]LanguageGuardModelStat, 0, len(m.stats))
	for _, s := range m.stats {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ModelID != out[j].ModelID {
			return out[i].ModelID < out[j].ModelID
		}
		return out[i].Role < out[j].Role
	})
	return out
}

// OverallRate returns the aggregate mismatch rate across all (model, role)
// cells (total mismatches / total checks; 0 when no checks have been
// recorded).
func (m *LanguageGuardMetrics) OverallRate() float64 {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	var checks, mismatches int64
	for _, s := range m.stats {
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

// LanguageGuardStats is the Agent's diagnostic accessor for the
// language-guard metric: it returns the
// process-wide per-(model, role) snapshot — for each (model, role), how
// many final messages the guard judged and how many were mismatches, with a
// mismatch rate per (model, role). A high rate for a model means it
// frequently replies in the wrong language. It is pure Go (no cgo), so it
// works in the native daemon, the CLI, and the WASM build alike.
func (a *Agent) LanguageGuardStats() []LanguageGuardModelStat {
	return GlobalLanguageGuardMetrics().Snapshot()
}
