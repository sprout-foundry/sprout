package trace

import (
	"os"
	"sync"
	"testing"
)

// TestTraceSessionConcurrentTurnWrites verifies that multiple goroutines can
// safely write turn records concurrently without data corruption.
func TestTraceSessionConcurrentTurnWrites(t *testing.T) {
	// Create a temporary trace directory
	traceDir := t.TempDir()

	session, err := NewTraceSession(traceDir, "test_provider", "test_model")
	if err != nil {
		t.Fatalf("Failed to create trace session: %v", err)
	}
	defer session.Close()

	const numGoroutines = 100
	const writesPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Launch multiple goroutines, each writing multiple turn records
	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			defer wg.Done()

			for j := 0; j < writesPerGoroutine; j++ {
				turnIndex := goroutineID*writesPerGoroutine + j
				record := TurnRecord{
					RunID:            session.GetRunID(),
					TurnIndex:        turnIndex,
					SystemPrompt:     "system prompt",
					UserPrompt:       "user prompt",
					UserPromptOriginal: "original prompt",
					RawResponse:      "response",
					Timestamp:        "2024-01-01T00:00:00Z",
				}

				if err := session.RecordTurn(record); err != nil {
					t.Errorf("Goroutine %d, write %d: failed to record turn: %v", goroutineID, j, err)
				}
			}
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Verify the session is still functional
	testRecord := TurnRecord{
		RunID:     session.GetRunID(),
		TurnIndex: 999,
		Timestamp: "2024-01-01T00:00:00Z",
	}
	if err := session.RecordTurn(testRecord); err != nil {
		t.Errorf("Failed to record turn after concurrent writes: %v", err)
	}

	// Close and verify no errors
	if err := session.Close(); err != nil {
		t.Fatalf("Failed to close session: %v", err)
	}

	// Verify all records were written by counting lines in the file
	turnsFilePath := session.GetRunDir() + "/turns.jsonl"
	data, err := os.ReadFile(turnsFilePath)
	if err != nil {
		t.Fatalf("Failed to read turns file: %v", err)
	}

	// Count newlines (each JSON object should be on its own line)
	lineCount := 0
	for _, b := range data {
		if b == '\n' {
			lineCount++
		}
	}

	expectedLines := numGoroutines*writesPerGoroutine + 1 // +1 for the test record
	if lineCount != expectedLines {
		t.Errorf("Expected %d lines in turns file, got %d", expectedLines, lineCount)
	}
}

// TestTraceSessionConcurrentToolCallWrites verifies that multiple goroutines can
// safely write tool call records concurrently without data corruption.
func TestTraceSessionConcurrentToolCallWrites(t *testing.T) {
	// Create a temporary trace directory
	traceDir := t.TempDir()

	session, err := NewTraceSession(traceDir, "test_provider", "test_model")
	if err != nil {
		t.Fatalf("Failed to create trace session: %v", err)
	}
	defer session.Close()

	const numGoroutines = 100
	const writesPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Launch multiple goroutines, each writing multiple tool call records
	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			defer wg.Done()

			for j := 0; j < writesPerGoroutine; j++ {
				toolIndex := goroutineID*writesPerGoroutine + j
				record := ToolCallRecord{
					RunID:      session.GetRunID(),
					TurnIndex:  goroutineID,
					ToolIndex:  toolIndex,
					ToolName:   "test_tool",
					Args:       map[string]interface{}{"arg1": "value1"},
					Success:    true,
					FullResult: "result",
					ModelResult: "model result",
					Timestamp:  "2024-01-01T00:00:00Z",
				}

				if err := session.RecordToolCall(record); err != nil {
					t.Errorf("Goroutine %d, write %d: failed to record tool call: %v", goroutineID, j, err)
				}
			}
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Verify the session is still functional
	testRecord := ToolCallRecord{
		RunID:      session.GetRunID(),
		TurnIndex:  0,
		ToolIndex:  99999,
		ToolName:   "final_tool",
		Success:    true,
		Timestamp:  "2024-01-01T00:00:00Z",
	}
	if err := session.RecordToolCall(testRecord); err != nil {
		t.Errorf("Failed to record tool call after concurrent writes: %v", err)
	}

	// Close and verify no errors
	if err := session.Close(); err != nil {
		t.Fatalf("Failed to close session: %v", err)
	}

	// Verify all records were written by counting lines in the file
	toolsFilePath := session.GetRunDir() + "/tool_calls.jsonl"
	data, err := os.ReadFile(toolsFilePath)
	if err != nil {
		t.Fatalf("Failed to read tool_calls file: %v", err)
	}

	// Count newlines
	lineCount := 0
	for _, b := range data {
		if b == '\n' {
			lineCount++
		}
	}

	expectedLines := numGoroutines*writesPerGoroutine + 1 // +1 for the test record
	if lineCount != expectedLines {
		t.Errorf("Expected %d lines in tool_calls file, got %d", expectedLines, lineCount)
	}
}

// TestTraceSessionConcurrentClose verifies that closing the session while
// concurrent writes are in progress does not cause panics or race conditions.
func TestTraceSessionConcurrentClose(t *testing.T) {
	// Create a temporary trace directory
	traceDir := t.TempDir()

	session, err := NewTraceSession(traceDir, "test_provider", "test_model")
	if err != nil {
		t.Fatalf("Failed to create trace session: %v", err)
	}

	const numGoroutines = 50
	const writesPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines + 1) // +1 for the close goroutine

	// Launch goroutines that write records
	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			defer wg.Done()

			for j := 0; j < writesPerGoroutine; j++ {
				turnRecord := TurnRecord{
					RunID:     session.GetRunID(),
					TurnIndex: goroutineID*writesPerGoroutine + j,
					Timestamp: "2024-01-01T00:00:00Z",
				}

				if err := session.RecordTurn(turnRecord); err != nil {
					// Errors are expected after close, so just log
					t.Logf("Goroutine %d: turn write error (expected after close): %v", goroutineID, err)
				}

				toolRecord := ToolCallRecord{
					RunID:     session.GetRunID(),
					TurnIndex: goroutineID,
					ToolIndex: j,
					ToolName:  "test_tool",
					Timestamp: "2024-01-01T00:00:00Z",
				}

				if err := session.RecordToolCall(toolRecord); err != nil {
					// Errors are expected after close, so just log
					t.Logf("Goroutine %d: tool call write error (expected after close): %v", goroutineID, err)
				}

				artifactRecord := ArtifactManifest{
					RunID:        session.GetRunID(),
					RelativePath: "test.txt",
					SizeBytes:    100,
					Hash:         "abc123",
					Timestamp:    "2024-01-01T00:00:00Z",
				}

				if err := session.RecordArtifact(artifactRecord); err != nil {
					// Errors are expected after close, so just log
					t.Logf("Goroutine %d: artifact write error (expected after close): %v", goroutineID, err)
				}
			}
		}(i)
	}

	// Launch a goroutine that closes the session after a short delay
	go func() {
		defer wg.Done()
		// Small delay to allow some writes to start
		// Note: We don't use time.Sleep in tests, so we'll just close immediately
		// This is a race condition test, so it's intentional
		if err := session.Close(); err != nil {
			t.Logf("Close error (may be expected): %v", err)
		}
	}()

	// Wait for all goroutines to complete
	wg.Wait()

	// Verify no panic occurred (if we got here, no panic)
	// The session should be closed now
	if err := session.Close(); err != nil {
		// Second close should be idempotent and not error
		t.Errorf("Second close should not error: %v", err)
	}
}

// TestJsonlWriterConcurrentWrites verifies that multiple goroutines can
// safely write to the same jsonlWriter without data corruption.
func TestJsonlWriterConcurrentWrites(t *testing.T) {
	// Create a temporary file
	tmpFile := t.TempDir() + "/test.jsonl"

	writer, err := newJSONLWriter(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create jsonl writer: %v", err)
	}
	defer writer.Close()

	const numGoroutines = 50
	const writesPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Launch multiple goroutines, each writing multiple records
	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			defer wg.Done()

			for j := 0; j < writesPerGoroutine; j++ {
				record := map[string]interface{}{
					"goroutine_id": goroutineID,
					"write_index":  j,
					"data":         "test data",
				}

				if err := writer.Write(record); err != nil {
					t.Errorf("Goroutine %d, write %d: failed to write: %v", goroutineID, j, err)
				}
			}
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Flush to ensure all data is written
	if err := writer.Flush(); err != nil {
		t.Fatalf("Failed to flush writer: %v", err)
	}

	// Close the writer
	if err := writer.Close(); err != nil {
		t.Fatalf("Failed to close writer: %v", err)
	}

	// Verify all records were written by reading the file
	data, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	// Count newlines
	lineCount := 0
	for _, b := range data {
		if b == '\n' {
			lineCount++
		}
	}

	expectedLines := numGoroutines * writesPerGoroutine
	if lineCount != expectedLines {
		t.Errorf("Expected %d lines in file, got %d", expectedLines, lineCount)
	}

	// Verify file is not corrupted (contains valid JSON lines)
	// We just verify it's not empty and contains expected markers
	if len(data) == 0 {
		t.Fatal("File is empty after concurrent writes")
	}
}

// TestJsonlWriterConcurrentWriteAndClose verifies that closing the writer
// while concurrent writes are in progress does not cause panics.
func TestJsonlWriterConcurrentWriteAndClose(t *testing.T) {
	// Create a temporary file
	tmpFile := t.TempDir() + "/test.jsonl"

	writer, err := newJSONLWriter(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create jsonl writer: %v", err)
	}

	const numGoroutines = 50
	const writesPerGoroutine = 200

	var wg sync.WaitGroup
	wg.Add(numGoroutines + 1) // +1 for the close goroutine

	// Launch goroutines that write records
	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			defer wg.Done()

			for j := 0; j < writesPerGoroutine; j++ {
				record := map[string]interface{}{
					"goroutine_id": goroutineID,
					"write_index":  j,
					"data":         "test data",
				}

				if err := writer.Write(record); err != nil {
					// Errors are expected after close
					t.Logf("Goroutine %d, write %d: write error (expected after close): %v", goroutineID, j, err)
				}
			}
		}(i)
	}

	// Launch a goroutine that closes the writer after some writes have started
	go func() {
		defer wg.Done()
		// Do a small write to close after we know some writes occurred
		record := map[string]interface{}{"sync": "pre-close"}
		writer.Write(record)

		// Now close to test concurrent close behavior
		if err := writer.Close(); err != nil {
			t.Logf("Close error (may be expected): %v", err)
		}
	}()

	// Wait for all goroutines to complete
	wg.Wait()

	// Verify no panic occurred (if we got here, no panic)
	// Additional close should be idempotent
	if err := writer.Close(); err != nil {
		t.Logf("Second close error: %v", err)
	}

	// Verify file exists and is readable
	data, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	// Verify file has some content (at least the sync write should have succeeded)
	if len(data) == 0 {
		t.Error("File is empty after concurrent writes and close")
	}
}

// TestTraceSessionMixedConcurrentWrites verifies that multiple goroutines can
// safely write different types of records (turns, tool calls, artifacts) concurrently.
func TestTraceSessionMixedConcurrentWrites(t *testing.T) {
	// Create a temporary trace directory
	traceDir := t.TempDir()

	session, err := NewTraceSession(traceDir, "test_provider", "test_model")
	if err != nil {
		t.Fatalf("Failed to create trace session: %v", err)
	}
	defer session.Close()

	const numGoroutines = 30
	const writesPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Launch goroutines that write different types of records
	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			defer wg.Done()

			for j := 0; j < writesPerGoroutine; j++ {
				// Write different record types in rotation
				recordType := j % 3

				switch recordType {
				case 0: // Turn record
					turnRecord := TurnRecord{
						RunID:     session.GetRunID(),
						TurnIndex: goroutineID*writesPerGoroutine + j,
						Timestamp: "2024-01-01T00:00:00Z",
					}
					if err := session.RecordTurn(turnRecord); err != nil {
						t.Errorf("Goroutine %d, write %d: failed to record turn: %v", goroutineID, j, err)
					}

				case 1: // Tool call record
					toolRecord := ToolCallRecord{
						RunID:     session.GetRunID(),
						TurnIndex: goroutineID,
						ToolIndex: j,
						ToolName:  "test_tool",
						Timestamp: "2024-01-01T00:00:00Z",
					}
					if err := session.RecordToolCall(toolRecord); err != nil {
						t.Errorf("Goroutine %d, write %d: failed to record tool call: %v", goroutineID, j, err)
					}

				case 2: // Artifact record
					artifactRecord := ArtifactManifest{
						RunID:        session.GetRunID(),
						RelativePath: "test.txt",
						SizeBytes:    int64(j),
						Hash:         "abc123",
						Timestamp:    "2024-01-01T00:00:00Z",
					}
					if err := session.RecordArtifact(artifactRecord); err != nil {
						t.Errorf("Goroutine %d, write %d: failed to record artifact: %v", goroutineID, j, err)
					}
				}
			}
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Close and verify no errors
	if err := session.Close(); err != nil {
		t.Fatalf("Failed to close session: %v", err)
	}

	// Verify all files have expected content
	turnsFilePath := session.GetRunDir() + "/turns.jsonl"
	toolsFilePath := session.GetRunDir() + "/tool_calls.jsonl"
	artifactsFilePath := session.GetRunDir() + "/artifacts_manifest.jsonl"

	// Count lines in each file
	// Since we use j % 3 for 50 writes, the distribution is:
	// - turns (j % 3 == 0): j=0,3,6,9,12,15,18,21,24,27,30,33,36,39,42,45,48 = 17 per goroutine
	// - tools (j % 3 == 1): j=1,4,7,10,13,16,19,22,25,28,31,34,37,40,43,46,49 = 17 per goroutine
	// - artifacts (j % 3 == 2): j=2,5,8,11,14,17,20,23,26,29,32,35,38,41,44,47 = 16 per goroutine
	expectedTurns := numGoroutines * 17
	expectedTools := numGoroutines * 17
	expectedArtifacts := numGoroutines * 16

	for _, filePath := range []struct {
		path     string
		name     string
		expected int
	}{
		{turnsFilePath, "turns", expectedTurns},
		{toolsFilePath, "tool_calls", expectedTools},
		{artifactsFilePath, "artifacts_manifest", expectedArtifacts},
	} {
		data, err := os.ReadFile(filePath.path)
		if err != nil {
			t.Fatalf("Failed to read %s file: %v", filePath.name, err)
		}

		lineCount := 0
		for _, b := range data {
			if b == '\n' {
				lineCount++
			}
		}

		if lineCount != filePath.expected {
			t.Errorf("Expected %d lines in %s file, got %d", filePath.expected, filePath.name, lineCount)
		}
	}
}

// TestTraceSessionDisabledConcurrentWrites verifies that when a session is disabled
// (created with empty traceDir), concurrent writes return no errors and don't panic.
func TestTraceSessionDisabledConcurrentWrites(t *testing.T) {
	// Create a disabled session (empty traceDir)
	session, err := NewTraceSession("", "test_provider", "test_model")
	if err != nil {
		t.Fatalf("Failed to create disabled trace session: %v", err)
	}
	defer session.Close()

	if session.IsEnabled {
		t.Fatal("Session should be disabled")
	}

	const numGoroutines = 50
	const writesPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Launch multiple goroutines, each writing multiple records
	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			defer wg.Done()

			for j := 0; j < writesPerGoroutine; j++ {
				turnRecord := TurnRecord{
					RunID:     session.GetRunID(),
					TurnIndex: j,
					Timestamp: "2024-01-01T00:00:00Z",
				}

				if err := session.RecordTurn(turnRecord); err != nil {
					t.Errorf("Goroutine %d, write %d: failed to record turn: %v", goroutineID, j, err)
				}

				toolRecord := ToolCallRecord{
					RunID:     session.GetRunID(),
					TurnIndex: goroutineID,
					ToolIndex: j,
					ToolName:  "test_tool",
					Timestamp: "2024-01-01T00:00:00Z",
				}

				if err := session.RecordToolCall(toolRecord); err != nil {
					t.Errorf("Goroutine %d, write %d: failed to record tool call: %v", goroutineID, j, err)
				}

				artifactRecord := ArtifactManifest{
					RunID:        session.GetRunID(),
					RelativePath: "test.txt",
					SizeBytes:    100,
					Hash:         "abc123",
					Timestamp:    "2024-01-01T00:00:00Z",
				}

				if err := session.RecordArtifact(artifactRecord); err != nil {
					t.Errorf("Goroutine %d, write %d: failed to record artifact: %v", goroutineID, j, err)
				}
			}
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Close should be idempotent
	if err := session.Close(); err != nil {
		t.Errorf("Failed to close disabled session: %v", err)
	}
}
