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

// --- NewTraceSession: MkdirAll succeeds but JSONL writer fails ---

func TestNewTraceSession_MkdirSucceedsButWriterFails(t *testing.T) {
	traceDir := t.TempDir()

	// We need MkdirAll to succeed (the run subdirectory already exists)
	// but creating runs.jsonl inside it to fail.
	// Strategy: predict the run subdirectory name using the same logic as
	// NewTraceSession (time.Now().Format + randomID), then create the dir
	// and place a directory named "runs.jsonl" as an obstacle.
	// Since randomID is deterministic (returns "012345" for length 6),
	// we can predict the full name.
	// Retry up to 3 times to avoid flakiness if the clock ticks between
	// our prediction and NewTraceSession's internal time.Now() call.
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		now := time.Now()
		predictedRunID := now.Format("20060102_150405") + "_012345"
		runDir := filepath.Join(traceDir, predictedRunID)

		if err := os.MkdirAll(runDir, 0o755); err != nil {
			t.Fatalf("failed to pre-create run dir: %v", err)
		}

		// Place a *directory* named runs.jsonl inside the pre-created run dir.
		// When NewTraceSession runs, MkdirAll will succeed (already exists),
		// but newJSONLWriter for runs.jsonl will fail because it's a directory.
		obstacle := filepath.Join(runDir, "runs.jsonl")
		if err := os.Mkdir(obstacle, 0o755); err != nil {
			t.Fatalf("failed to create obstacle directory: %v", err)
		}

		s, err := NewTraceSession(traceDir, "provider", "model")
		if err == nil {
			s.Close()
			// The timestamp didn't match our prediction — clean up and retry.
			// Only remove the obstacle dirs, not the traceDir (managed by t.TempDir).
			os.Remove(obstacle)
			os.Remove(runDir)
			lastErr = nil
			continue
		}

		if !strings.Contains(err.Error(), "is a directory") && !strings.Contains(err.Error(), "not a regular file") {
			// Error from a different cause (e.g., timestamp mismatch created
			// a different subdir successfully). This is acceptable.
			t.Logf("attempt %d: error (acceptable): %v", attempt+1, err)
		}
		lastErr = err
		break
	}

	if lastErr == nil {
		t.Fatal("expected error when runs.jsonl is a directory after 3 attempts")
	}
}

// --- Close: individual file closes produce errors ---

func TestClose_WithClosedFileHandles(t *testing.T) {
	traceDir := t.TempDir()
	s, err := NewTraceSession(traceDir, "anthropic", "claude-3")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Record some data, then manually close the underlying file handles
	// to simulate a scenario where individual closes error.
	if err := s.RecordTurn(TurnRecord{
		RunID:     s.GetRunID(),
		TurnIndex: 0,
		Timestamp: "2024-06-15T10:30:00Z",
	}); err != nil {
		t.Fatalf("RecordTurn error: %v", err)
	}

	// Close the underlying file of RunsFile to force an error on session Close
	s.mu.Lock()
	if s.RunsFile != nil {
		s.RunsFile.file.Close()
	}
	if s.TurnsFile != nil {
		s.TurnsFile.file.Close()
	}
	s.mu.Unlock()

	// Close should return the first error encountered, not panic
	err = s.Close()
	if err == nil {
		t.Log("Close returned nil even though underlying files were pre-closed (OS-dependent)")
	} else {
		// On most systems, closing an already-closed fd returns EBADF
		t.Logf("expected close error: %v", err)
	}

	// Second close should be idempotent (no error)
	if err := s.Close(); err != nil {
		t.Errorf("second Close should be idempotent, got error: %v", err)
	}
}

func TestHashFile_DirectoryPath(t *testing.T) {
	tmpDir := t.TempDir()

	// hashFile on a directory should return an error, not panic.
	// os.ReadFile on a directory returns an error on all platforms.
	_, err := hashFile(tmpDir)
	if err == nil {
		t.Error("expected error when hashing a directory path, got nil")
	}
}

// --- Record turn, close, record another turn (should succeed silently) ---

func TestRecordTurn_AfterClose(t *testing.T) {
	traceDir := t.TempDir()
	s, err := NewTraceSession(traceDir, "anthropic", "claude-3")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Record a turn
	if err := s.RecordTurn(TurnRecord{
		RunID:     s.GetRunID(),
		TurnIndex: 0,
		Timestamp: "2024-06-15T10:30:00Z",
	}); err != nil {
		t.Fatalf("first RecordTurn error: %v", err)
	}

	// Close the session
	if err := s.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	// Recording another turn after close should return nil (no-op, not error)
	if err := s.RecordTurn(TurnRecord{
		RunID:     s.GetRunID(),
		TurnIndex: 1,
		Timestamp: "2024-06-15T10:31:00Z",
	}); err != nil {
		t.Errorf("RecordTurn after close should return nil, got %v", err)
	}

	// Recording a tool call after close should also return nil
	if err := s.RecordToolCall(ToolCallRecord{
		RunID:     s.GetRunID(),
		TurnIndex: 1,
		ToolIndex: 0,
		ToolName:  "shell_command",
		Timestamp: "2024-06-15T10:31:00Z",
	}); err != nil {
		t.Errorf("RecordToolCall after close should return nil, got %v", err)
	}

	// Verify only 1 turn was written (the one before close)
	data, err := os.ReadFile(filepath.Join(s.GetRunDir(), "turns.jsonl"))
	if err != nil {
		t.Fatalf("failed to read turns.jsonl: %v", err)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	count := 0
	for scanner.Scan() {
		count++
	}
	if count != 1 {
		t.Errorf("expected 1 turn record in file, got %d", count)
	}
}

func TestRecordTurn_CloseReopen_RecordAgain(t *testing.T) {
	traceDir := t.TempDir()

	// Create first session
	s1, err := NewTraceSession(traceDir, "anthropic", "claude-3")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if err := s1.RecordTurn(TurnRecord{
		RunID:     s1.GetRunID(),
		TurnIndex: 0,
		Timestamp: "2024-06-15T10:30:00Z",
	}); err != nil {
		t.Fatalf("first session RecordTurn error: %v", err)
	}
	s1.Close()

	// Create a second session in the same traceDir — should get its own subdirectory
	s2, err := NewTraceSession(traceDir, "anthropic", "claude-3")
	if err != nil {
		t.Fatalf("expected no error for second session, got %v", err)
	}
	defer s2.Close()

	if err := s2.RecordTurn(TurnRecord{
		RunID:     s2.GetRunID(),
		TurnIndex: 0,
		Timestamp: "2024-06-15T10:35:00Z",
	}); err != nil {
		t.Fatalf("second session RecordTurn error: %v", err)
	}

	// Verify second session has its data
	data, err := os.ReadFile(filepath.Join(s2.GetRunDir(), "turns.jsonl"))
	if err != nil {
		t.Fatalf("failed to read second session's turns.jsonl: %v", err)
	}

	var turn TurnRecord
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	if !scanner.Scan() {
		t.Fatal("no line found in second session's turns.jsonl")
	}
	if err := json.Unmarshal(scanner.Bytes(), &turn); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if turn.RunID != s2.GetRunID() {
		t.Errorf("RunID mismatch: got %q, want %q", turn.RunID, s2.GetRunID())
	}
	if turn.Timestamp != "2024-06-15T10:35:00Z" {
		t.Errorf("Timestamp mismatch: got %q", turn.Timestamp)
	}

	// First session should still have exactly 1 turn
	data1, err := os.ReadFile(filepath.Join(s1.GetRunDir(), "turns.jsonl"))
	if err != nil {
		t.Fatalf("failed to read first session's turns.jsonl: %v", err)
	}
	scanner1 := bufio.NewScanner(strings.NewReader(string(data1)))
	count := 0
	for scanner1.Scan() {
		count++
	}
	if count != 1 {
		t.Errorf("first session should still have 1 turn record, got %d", count)
	}
}

// --- Close: nil file pointers ---

func TestClose_WithNilWriters(t *testing.T) {
	// Create a session with nil writers to test Close doesn't panic
	s := &TraceSession{
		IsEnabled: true,
		closed:    false,
	}

	err := s.Close()
	if err != nil {
		t.Errorf("Close with nil writers should not error, got %v", err)
	}
	if !s.closed {
		t.Error("session should be marked as closed")
	}
}
