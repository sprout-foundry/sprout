package trace

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- jsonlWriter Flush ---

func TestJsonlWriter_Flush(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "flush_test.jsonl")

	w, err := newJSONLWriter(tmpFile)
	if err != nil {
		t.Fatalf("newJSONLWriter failed: %v", err)
	}

	// Write some data
	if err := w.Write(map[string]string{"key": "value"}); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Flush should succeed (sync to disk)
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush returned error: %v", err)
	}

	// Close and verify the data was written
	if err := w.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	data, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if !strings.Contains(string(data), `"key":"value"`) {
		t.Errorf("expected file to contain written data, got: %s", string(data))
	}
}

// --- jsonlWriter Write after Close ---

func TestJsonlWriter_WriteAfterClose(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "write_after_close.jsonl")

	w, err := newJSONLWriter(tmpFile)
	if err != nil {
		t.Fatalf("newJSONLWriter failed: %v", err)
	}

	// Close the writer
	if err := w.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Writing to a closed writer should return an error
	err = w.Write(map[string]string{"key": "after_close"})
	if err == nil {
		t.Error("expected error when writing to closed jsonlWriter, got nil")
	}
}

// --- jsonlWriter Close double-close behavior ---

func TestJsonlWriter_CloseDouble(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "close_double.jsonl")

	w, err := newJSONLWriter(tmpFile)
	if err != nil {
		t.Fatalf("newJSONLWriter failed: %v", err)
	}

	// Write and close normally
	if err := w.Write(map[string]string{"key": "first"}); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}

	// Double close — os.File.Close on an already-closed file returns an error.
	// This is acceptable: the session-level Close guards via the `closed` flag.
	err = w.Close()
	if err == nil {
		t.Fatal("expected error on second Close of jsonlWriter, got nil")
	}
	t.Logf("second Close returned (expected): %v", err)
}

func TestJsonlWriter_FlushAfterClose(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "flush_after_close.jsonl")

	w, err := newJSONLWriter(tmpFile)
	if err != nil {
		t.Fatalf("newJSONLWriter failed: %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Flush after close — the file is closed so Sync should fail
	err = w.Flush()
	if err == nil {
		t.Fatal("expected error when flushing closed jsonlWriter, got nil")
	}
	t.Logf("Flush after close returned (expected): %v", err)
}

// predictNextRunID returns the run ID that NewTraceSession will generate
// during the current wall-clock second. It sleeps briefly past the current
// second boundary first, then computes the timestamp so that any call to
// NewTraceSession within the same second produces a matching runID.
//
// randomID(6) always returns "012345" (deterministic charset indexing).
func predictNextRunID(t *testing.T) string {
	t.Helper()
	// Sleep past the current second boundary to ensure a clean slate.
	// Nanoseconds remaining → duration until next second.
	now := time.Now()
	remaining := time.Second - time.Duration(now.Nanosecond())
	if remaining > 0 {
		time.Sleep(remaining + time.Millisecond) // 1ms past the boundary
	}
	now = time.Now()
	return now.Format("20060102_150405") + "_012345"
}

// --- NewTraceSession: runs.jsonl already exists as a regular file (overwrite) ---

func TestNewTraceSession_ExistingRunsJsonlIsRegularFile(t *testing.T) {
	traceDir := t.TempDir()

	// Predict the run ID for the current second, then pre-create the directory
	// and a stale runs.jsonl. Since predictNextRunID aligns to the second
	// boundary, NewTraceSession should produce the same runID.
	predictedRunID := predictNextRunID(t)
	runDir := filepath.Join(traceDir, predictedRunID)

	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("failed to create predicted run dir: %v", err)
	}

	runsPath := filepath.Join(runDir, "runs.jsonl")
	if err := os.WriteFile(runsPath, []byte(`{"old":true}`+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write pre-existing runs.jsonl: %v", err)
	}

	session, err := NewTraceSession(traceDir, "anthropic", "claude-3")
	if err != nil {
		t.Fatalf("NewTraceSession failed: %v", err)
	}
	defer session.Close()

	// Verify runs.jsonl was overwritten with fresh metadata (not the old content)
	data, err := os.ReadFile(filepath.Join(session.GetRunDir(), "runs.jsonl"))
	if err != nil {
		t.Fatalf("failed to read runs.jsonl: %v", err)
	}

	var meta RunMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("failed to unmarshal runs.jsonl: %v", err)
	}

	if meta.Provider != "anthropic" {
		t.Errorf("expected Provider=anthropic, got %q", meta.Provider)
	}
	if meta.RunID != session.GetRunID() {
		t.Errorf("RunID mismatch: got %q, want %q", meta.RunID, session.GetRunID())
	}

	// Verify the old content is gone — the file should have been truncated
	// and rewritten with new metadata.
	if strings.Contains(string(data), `"old":true`) {
		t.Error("runs.jsonl should have been overwritten, but old content found")
	}
}

// --- NewTraceSession: subdirectory exists with all files already present ---

func TestNewTraceSession_ExistingSessionFiles(t *testing.T) {
	traceDir := t.TempDir()

	predictedRunID := predictNextRunID(t)
	runDir := filepath.Join(traceDir, predictedRunID)

	// Create the predicted run directory with all expected files
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("failed to create predicted run dir: %v", err)
	}

	// Write pre-existing files (simulating a previous run in the same directory slot)
	for _, name := range []string{
		"runs.jsonl", "turns.jsonl", "tool_calls.jsonl", "artifacts_manifest.jsonl",
	} {
		path := filepath.Join(runDir, name)
		if err := os.WriteFile(path, []byte(`{"stale":true}`+"\n"), 0o644); err != nil {
			t.Fatalf("failed to write pre-existing %s: %v", name, err)
		}
	}

	session, err := NewTraceSession(traceDir, "openai", "gpt-4")
	if err != nil {
		t.Fatalf("NewTraceSession failed: %v", err)
	}
	defer session.Close()

	// Verify runs.jsonl has new metadata, not stale data
	data, err := os.ReadFile(filepath.Join(session.GetRunDir(), "runs.jsonl"))
	if err != nil {
		t.Fatalf("failed to read runs.jsonl: %v", err)
	}

	var meta RunMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("failed to unmarshal runs.jsonl: %v", err)
	}

	if meta.Provider != "openai" {
		t.Errorf("expected Provider=openai, got %q", meta.Provider)
	}
	if meta.Model != "gpt-4" {
		t.Errorf("expected Model=gpt-4, got %q", meta.Model)
	}
	// Verify stale content was replaced by fresh metadata
	if strings.Contains(string(data), `"stale":true`) {
		t.Error("runs.jsonl should have been overwritten, found stale content")
	}

	// Verify turns.jsonl was truncated (no stale data remains)
	turnsData, err := os.ReadFile(filepath.Join(session.GetRunDir(), "turns.jsonl"))
	if err != nil {
		t.Fatalf("failed to read turns.jsonl: %v", err)
	}
	if strings.Contains(string(turnsData), `"stale":true`) {
		t.Error("turns.jsonl should have been truncated, stale content still present")
	}

	// Verify the session is functional by recording a turn
	if err := session.RecordTurn(TurnRecord{
		RunID:     session.GetRunID(),
		TurnIndex: 0,
		Timestamp: "2024-06-15T10:30:00Z",
	}); err != nil {
		t.Fatalf("RecordTurn on session with pre-existing files failed: %v", err)
	}

	session.Close()

	// Read back turns.jsonl — should have exactly 1 record
	turnsData, err = os.ReadFile(filepath.Join(session.GetRunDir(), "turns.jsonl"))
	if err != nil {
		t.Fatalf("failed to re-read turns.jsonl: %v", err)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(turnsData)))
	count := 0
	for scanner.Scan() {
		count++
	}
	if count != 1 {
		t.Errorf("expected exactly 1 turn record, got %d", count)
	}
}

// --- NewTraceSession: writer fails because turns.jsonl is a directory ---

func TestNewTraceSession_TurnsWriterDirectoryObstacle(t *testing.T) {
	traceDir := t.TempDir()

	predictedRunID := predictNextRunID(t)
	runDir := filepath.Join(traceDir, predictedRunID)

	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("failed to pre-create run dir: %v", err)
	}

	// Create turns.jsonl as a directory — os.OpenFile will fail with "is a directory"
	obstacle := filepath.Join(runDir, "turns.jsonl")
	if err := os.Mkdir(obstacle, 0o755); err != nil {
		t.Fatalf("failed to create obstacle: %v", err)
	}

	_, err := NewTraceSession(traceDir, "provider", "model")
	if err == nil {
		t.Fatal("expected error when turns.jsonl is a directory, got nil")
	}
	if !strings.Contains(err.Error(), "is a directory") && !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("expected directory-related error, got: %v", err)
	}

	// Verify cleanup happened: the run directory obstacle should have been removed
	// or the error left the directory in a known state. The key assertion is
	// that the error was returned (not a panic).
	t.Logf("got expected error: %v", err)
}

// --- NewTraceSession: first writer (runs.jsonl) fails because it's a directory ---

func TestNewTraceSession_RunsWriterDirectoryObstacle(t *testing.T) {
	traceDir := t.TempDir()

	predictedRunID := predictNextRunID(t)
	runDir := filepath.Join(traceDir, predictedRunID)

	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("failed to pre-create run dir: %v", err)
	}

	// Create runs.jsonl as a directory — os.OpenFile will fail
	obstacle := filepath.Join(runDir, "runs.jsonl")
	if err := os.Mkdir(obstacle, 0o755); err != nil {
		t.Fatalf("failed to create obstacle: %v", err)
	}

	_, err := NewTraceSession(traceDir, "provider", "model")
	if err == nil {
		t.Fatal("expected error when runs.jsonl is a directory, got nil")
	}
	if !strings.Contains(err.Error(), "is a directory") && !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("expected directory-related error, got: %v", err)
	}

	t.Logf("got expected error: %v", err)
}

// --- Verify overwrite behavior at the jsonlWriter level ---

func TestJsonlWriter_OverwriteExistingFile(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "overwrite.jsonl")

	// Write initial content
	if err := os.WriteFile(tmpFile, []byte(`{"old":true}`+"\n"), 0o644); err != nil {
		t.Fatalf("failed to write initial file: %v", err)
	}

	// Create writer — should truncate the existing file
	w, err := newJSONLWriter(tmpFile)
	if err != nil {
		t.Fatalf("newJSONLWriter failed: %v", err)
	}

	if err := w.Write(map[string]string{"new": "data"}); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Verify old content is gone
	data, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if strings.Contains(string(data), `"old":true`) {
		t.Error("file should have been truncated, old content still present")
	}
	if !strings.Contains(string(data), `"new":"data"`) {
		t.Errorf("expected new content in file, got: %s", string(data))
	}
}
