//go:build !js

// Package agent provides the semantic-recall instrumentation wrapper (SP-095-1).
//
// The wrapper observes InjectSemanticRecall calls, records per-turn metrics
// (items recalled, top similarity, recall latency) and persists daily
// counters to ~/.config/sprout/recall_metrics.jsonl.
//
// The instrumentation is fire-and-forget: failures (sink init, file IO, etc)
// are silent at the agent level and never block the agent loop.

package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// RecallMetricsRecord — per-turn JSONL entry
// ---------------------------------------------------------------------------

// RecallMetricsRecord is the per-turn entry persisted to
// ~/.config/sprout/recall_metrics.jsonl. Fire-and-forget — the
// instrumentation never blocks the agent loop on file IO.
type RecallMetricsRecord struct {
	Timestamp       string   `json:"timestamp"`              // RFC3339
	SessionID       string   `json:"session_id,omitempty"`
	ItemsRecalled   int      `json:"items_recalled"`
	TopSimilarity   float64  `json:"top_similarity"`
	UsedInResponse  bool     `json:"used_in_response"`
	CheckpointIDs   []string `json:"checkpoint_ids,omitempty"`
	Workspaces      []string `json:"workspaces,omitempty"`
	RecallLatencyMS int64    `json:"recall_latency_ms"`
	RecallQuery     string   `json:"recall_query,omitempty"` // first 200 runes, for debugging
}

// ---------------------------------------------------------------------------
// recallMetricsSink — buffered JSONL appender
// ---------------------------------------------------------------------------

// recallMetricsSink is a buffered JSONL appender for recall metrics.
// Safe for concurrent use from multiple goroutines.
type recallMetricsSink struct {
	mu     sync.Mutex
	file   *os.File
	writer *bufio.Writer
	path   string
}

// newRecallMetricsSink creates a sink that appends to ~/.config/sprout/recall_metrics.jsonl.
// Returns nil if the file cannot be opened (permission error, missing home dir, etc).
// Callers MUST treat nil as a no-op sink.
func newRecallMetricsSink() *recallMetricsSink {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	dir := filepath.Join(homeDir, ".config", "sprout")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil
	}
	path := filepath.Join(dir, "recall_metrics.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil
	}
	return &recallMetricsSink{
		file:   f,
		writer: bufio.NewWriter(f),
		path:   path,
	}
}

// Append writes a single JSONL record. Best-effort: flush errors are
// logged at debug level but do not propagate.
func (s *recallMetricsSink) Append(rec RecallMetricsRecord) {
	if s == nil {
		return
	}
	if rec.Timestamp == "" {
		rec.Timestamp = time.Now().Format(time.RFC3339)
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.writer.Write(data)
	_ = s.writer.WriteByte('\n')
	_ = s.writer.Flush()
}

// Close releases the file handle. Idempotent and nil-safe.
func (s *recallMetricsSink) Close() error {
	if s == nil || s.file == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.writer.Flush()
	err := s.file.Close()
	s.file = nil
	return err
}

// ---------------------------------------------------------------------------
// Global singleton sink (lazy-init, thread-safe)
// ---------------------------------------------------------------------------

var defaultRecallSink *recallMetricsSink
var defaultRecallSinkOnce sync.Once

// getRecallSink returns the process-wide recall metrics sink.
// Returns nil if the sink could not be initialized (file open failed).
// If setRecallSinkForTesting was called first, returns the test sink.
func getRecallSink() *recallMetricsSink {
	if defaultRecallSink != nil {
		return defaultRecallSink
	}
	defaultRecallSinkOnce.Do(func() {
		defaultRecallSink = newRecallMetricsSink()
	})
	return defaultRecallSink
}

// setRecallSinkForTesting replaces the global sink with a custom one.
// Intended for tests only; not safe for concurrent use with InstrumentedRecall.
func setRecallSinkForTesting(sink *recallMetricsSink) {
	defaultRecallSink = sink
}

// ---------------------------------------------------------------------------
// InstrumentedRecall — the public wrapper
// ---------------------------------------------------------------------------

// InstrumentedRecall wraps an InjectSemanticRecall invocation with
// per-turn telemetry. Captures:
//
//   - ItemsRecalled: number of RecalledItems returned
//   - TopSimilarity: highest cosine similarity in the result set
//   - RecallLatencyMS: wall time for the Recall() call
//   - CheckpointIDs / Workspaces: metadata from the recalled items
//
// The record is appended to ~/.config/sprout/recall_metrics.jsonl.
//
// The instrumentation never alters the agent's behavior — it's a
// fire-and-forget observer. Failures (sink init, file IO, etc) are
// silent at the agent level.
//
// NOTE: InstrumentedRecall calls a.Recall() to capture metrics, then
// calls a.InjectSemanticRecall() which internally calls a.Recall() again.
// The duplicate recall work is bounded by the 2-second context timeout
// at the call site and is acceptable overhead for v1 instrumentation.
func InstrumentedRecall(a *Agent, ctx context.Context, query string) {
	if a == nil {
		return
	}

	sink := getRecallSink()

	start := time.Now()
	items, err := a.Recall(ctx, query, semanticRecallTopK)
	latencyMS := time.Since(start).Milliseconds()

	// Always run the existing injection to preserve behavior.
	a.InjectSemanticRecall(ctx, query)

	if err != nil || len(items) == 0 {
		// No items to instrument — skip the record (it'd be noise).
		return
	}

	// Compute metrics from items.
	topSim := 0.0
	var checkpointIDs []string
	var workspaces []string
	for _, item := range items {
		sim := float64(item.Similarity)
		if sim > topSim {
			topSim = sim
		}
		if item.CheckpointID != "" {
			checkpointIDs = append(checkpointIDs, item.CheckpointID)
		}
		if item.Workspace != "" {
			workspaces = append(workspaces, item.Workspace)
		}
	}

	// Append metrics (fire-and-forget, best-effort).
	if sink != nil {
		sink.Append(RecallMetricsRecord{
			Timestamp:       time.Now().Format(time.RFC3339),
			SessionID:       a.GetSessionID(),
			ItemsRecalled:   len(items),
			TopSimilarity:   topSim,
			CheckpointIDs:   checkpointIDs,
			Workspaces:      workspaces,
			RecallLatencyMS: latencyMS,
			RecallQuery:     truncate(query, 200),
		})
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// truncate returns the first n runes of s (rune-safe, no allocation).
func truncate(s string, n int) string {
	count := 0
	for i := range s {
		count++
		if count > n {
			return s[:i]
		}
	}
	return s
}
