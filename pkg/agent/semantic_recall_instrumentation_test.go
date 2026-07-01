//go:build !js

package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/embedding"
)

// ---------------------------------------------------------------------------
// TestRecallMetricsSink_RoundTrip — write a record, read it back, parse JSONL
// ---------------------------------------------------------------------------

func TestRecallMetricsSink_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "recall_metrics.jsonl")

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}

	sink := &recallMetricsSink{
		file:   f,
		writer: bufio.NewWriter(f),
		path:   path,
	}

	rec := RecallMetricsRecord{
		Timestamp:       "2026-01-15T10:00:00Z",
		SessionID:       "test-session-123",
		ItemsRecalled:   3,
		TopSimilarity:   0.85,
		UsedInResponse:  true,
		CheckpointIDs:   []string{"cp-a", "cp-b", "cp-c"},
		Workspaces:      []string{"/repo/x"},
		RecallLatencyMS: 42,
		RecallQuery:     "how do I test this",
	}

	sink.Append(rec)
	err = sink.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Read back the file.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d: %q", len(lines), lines)
	}

	var got RecallMetricsRecord
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatalf("failed to parse JSONL line: %v", err)
	}

	if got.Timestamp != "2026-01-15T10:00:00Z" {
		t.Errorf("Timestamp = %q, want %q", got.Timestamp, "2026-01-15T10:00:00Z")
	}
	if got.SessionID != "test-session-123" {
		t.Errorf("SessionID = %q, want %q", got.SessionID, "test-session-123")
	}
	if got.ItemsRecalled != 3 {
		t.Errorf("ItemsRecalled = %d, want 3", got.ItemsRecalled)
	}
	if got.TopSimilarity != 0.85 {
		t.Errorf("TopSimilarity = %f, want 0.85", got.TopSimilarity)
	}
	if !got.UsedInResponse {
		t.Error("UsedInResponse should be true")
	}
	if len(got.CheckpointIDs) != 3 {
		t.Errorf("CheckpointIDs = %v, want 3 items", got.CheckpointIDs)
	}
	if got.RecallLatencyMS != 42 {
		t.Errorf("RecallLatencyMS = %d, want 42", got.RecallLatencyMS)
	}
	if got.RecallQuery != "how do I test this" {
		t.Errorf("RecallQuery = %q, want %q", got.RecallQuery, "how do I test this")
	}
}

// ---------------------------------------------------------------------------
// TestRecallMetricsSink_Append_NilSafe — calling Append on nil sink is a no-op
// ---------------------------------------------------------------------------

func TestRecallMetricsSink_Append_NilSafe(t *testing.T) {
	var s *recallMetricsSink
	// Should not panic.
	s.Append(RecallMetricsRecord{
		Timestamp:     time.Now().Format(time.RFC3339),
		ItemsRecalled: 1,
	})
}

// ---------------------------------------------------------------------------
// TestRecallMetricsSink_Close_NilSafe — calling Close on nil sink is a no-op
// ---------------------------------------------------------------------------

func TestRecallMetricsSink_Close_NilSafe(t *testing.T) {
	var s *recallMetricsSink
	// Should not panic.
	_ = s.Close()
}

// ---------------------------------------------------------------------------
// TestRecallMetricsSink_MultipleAppends — multiple records produce multiple lines
// ---------------------------------------------------------------------------

func TestRecallMetricsSink_MultipleAppends(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "recall_metrics.jsonl")

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}

	sink := &recallMetricsSink{
		file:   f,
		writer: bufio.NewWriter(f),
		path:   path,
	}

	for i := 0; i < 5; i++ {
		sink.Append(RecallMetricsRecord{
			Timestamp:       time.Now().Format(time.RFC3339),
			ItemsRecalled:   i + 1,
			TopSimilarity:   0.5 + float64(i)*0.1,
			RecallLatencyMS: int64(10 + i),
		})
	}
	sink.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 5 {
		t.Fatalf("expected 5 lines, got %d", len(lines))
	}

	// Verify each line is valid JSON.
	for i, line := range lines {
		var rec RecallMetricsRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Errorf("line %d failed to parse: %v (line: %s)", i, err, line)
		}
	}
}

// ---------------------------------------------------------------------------
// TestRecallMetricsSink_AutoTimestamp — empty timestamp gets auto-filled
// ---------------------------------------------------------------------------

func TestRecallMetricsSink_AutoTimestamp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "recall_metrics.jsonl")

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}

	sink := &recallMetricsSink{
		file:   f,
		writer: bufio.NewWriter(f),
		path:   path,
	}

	sink.Append(RecallMetricsRecord{
		ItemsRecalled:   2,
		TopSimilarity:   0.7,
		RecallLatencyMS: 15,
	})
	sink.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	var rec RecallMetricsRecord
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &rec); err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if rec.Timestamp == "" {
		t.Error("Timestamp should be auto-filled, got empty string")
	}
}

// ---------------------------------------------------------------------------
// TestInstrumentedRecall_NilAgent — no-op when agent is nil
// ---------------------------------------------------------------------------

func TestInstrumentedRecall_NilAgent(t *testing.T) {
	// Should not panic.
	InstrumentedRecall(nil, context.Background(), "test query")
}

// ---------------------------------------------------------------------------
// TestInstrumentedRecall_NoEmbeddingManager — short-circuits, no record written
// ---------------------------------------------------------------------------

func TestInstrumentedRecall_NoEmbeddingManager(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "recall_metrics.jsonl")

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}

	sink := &recallMetricsSink{
		file:   f,
		writer: bufio.NewWriter(f),
		path:   path,
	}
	setRecallSinkForTesting(sink)
	t.Cleanup(func() { setRecallSinkForTesting(nil) })

	a := &Agent{}
	// No embedding manager — Recall returns (nil, nil)
	InstrumentedRecall(a, context.Background(), "test query")
	sink.Close()

	// No record should be written (no items to instrument).
	data, _ := os.ReadFile(path)
	if len(strings.TrimSpace(string(data))) > 0 {
		t.Errorf("expected no records when there's no embedding manager, got: %s", data)
	}
}

// ---------------------------------------------------------------------------
// TestInstrumentedRecall_RecordsMetrics — full path with mock embedding manager
// ---------------------------------------------------------------------------

func TestInstrumentedRecall_RecordsMetrics(t *testing.T) {
	ctx := context.Background()

	// Build an EmbeddingManager with a mock provider.
	mgr := newTestEmbeddingMgr(t)
	defer mgr.Close()

	// Populate the conversation store with test records.
	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		t.Fatalf("failed to get conversation store: %v", err)
	}

	now := time.Now().UTC()
	provider := store.Provider()

	embed1, _ := provider.Embed(ctx, "first recalled summary about testing")
	embed2, _ := provider.Embed(ctx, "second recalled summary about auth")

	records := []embedding.VectorRecord{
		{
			ID:        "rollup:cp-1",
			Signature: "first recalled summary about testing",
			Embedding: embed1,
			IndexedAt: now.Add(-1 * 24 * time.Hour),
			Metadata: map[string]interface{}{
				"sessionId":     "test-session",
				"checkpoint_id": "cp-1",
				"level":         float64(0),
				"workingDir":    "/repo/test",
			},
		},
		{
			ID:        "rollup:cp-2",
			Signature: "second recalled summary about auth",
			Embedding: embed2,
			IndexedAt: now.Add(-2 * 24 * time.Hour),
			Metadata: map[string]interface{}{
				"sessionId":     "test-session",
				"checkpoint_id": "cp-2",
				"level":         float64(0),
				"workingDir":    "/repo/test",
			},
		},
	}

	if err := store.Store(records); err != nil {
		t.Fatalf("failed to store test records: %v", err)
	}

	// Wire the manager into an Agent and set the session ID.
	a := &Agent{}
	a.embeddingMgr = mgr
	a.SetSessionID("test-session")

	// Create a test sink.
	dir := t.TempDir()
	path := filepath.Join(dir, "recall_metrics.jsonl")

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}

	sink := &recallMetricsSink{
		file:   f,
		writer: bufio.NewWriter(f),
		path:   path,
	}
	setRecallSinkForTesting(sink)
	t.Cleanup(func() { setRecallSinkForTesting(nil) })

	// Run the instrumented recall.
	InstrumentedRecall(a, ctx, "testing auth config")
	sink.Close()

	// Read and verify the record.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 record, got %d lines", len(lines))
	}

	var rec RecallMetricsRecord
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("failed to parse JSONL record: %v (line: %s)", err, lines[0])
	}

	// Verify key fields.
	if rec.SessionID != "test-session" {
		t.Errorf("SessionID = %q, want %q", rec.SessionID, "test-session")
	}
	if rec.ItemsRecalled < 1 {
		t.Errorf("ItemsRecalled = %d, want >= 1", rec.ItemsRecalled)
	}
	if rec.TopSimilarity <= 0 {
		t.Errorf("TopSimilarity = %f, want > 0", rec.TopSimilarity)
	}
	if rec.RecallLatencyMS < 0 {
		t.Errorf("RecallLatencyMS = %d, want >= 0", rec.RecallLatencyMS)
	}
	if rec.RecallQuery != "testing auth config" {
		t.Errorf("RecallQuery = %q, want %q", rec.RecallQuery, "testing auth config")
	}
	if len(rec.CheckpointIDs) == 0 {
		t.Error("CheckpointIDs should not be empty when items are recalled")
	}
}

// ---------------------------------------------------------------------------
// TestTruncate — boundary cases
// ---------------------------------------------------------------------------

func TestTruncate(t *testing.T) {
	tests := []struct {
		name  string
		input string
		n     int
		want  string
	}{
		{
			name:  "shorter than limit",
			input: "hello",
			n:     10,
			want:  "hello",
		},
		{
			name:  "exact length",
			input: "hello",
			n:     5,
			want:  "hello",
		},
		{
			name:  "longer than limit",
			input: "hello world",
			n:     5,
			want:  "hello",
		},
		{
			name:  "empty string",
			input: "",
			n:     5,
			want:  "",
		},
		{
			name:  "zero limit",
			input: "hello",
			n:     0,
			want:  "",
		},
		{
			name:  "negative limit",
			input: "hello",
			n:     -1,
			want:  "",
		},
		{
			name:  "utf8 runes",
			input: "héllo wörld",
			n:     6,
			want:  "héllo ",
		},
		{
			name:  "emoji",
			input: "🎉🚀✨",
			n:     2,
			want:  "🎉🚀",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := truncate(tc.input, tc.n)
			if got != tc.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tc.input, tc.n, got, tc.want)
			}
		})
	}
}
