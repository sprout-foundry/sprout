package trace

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

// --- NewTraceSession: tools.jsonl writer fails (later writer error, tests defer cleanup) ---

func TestNewTraceSession_ToolsWriterFails(t *testing.T) {
	traceDir := t.TempDir()

	predictedRunID := predictNextRunID(t)
	runDir := filepath.Join(traceDir, predictedRunID)

	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("failed to pre-create run dir: %v", err)
	}

	// Create tool_calls.jsonl as a directory — os.OpenFile will fail
	obstacle := filepath.Join(runDir, "tool_calls.jsonl")
	if err := os.Mkdir(obstacle, 0o755); err != nil {
		t.Fatalf("failed to create obstacle: %v", err)
	}

	_, err := NewTraceSession(traceDir, "provider", "model")
	if err == nil {
		t.Fatal("expected error when tool_calls.jsonl is a directory, got nil")
	}
	if !strings.Contains(err.Error(), "is a directory") && !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("expected directory-related error, got: %v", err)
	}
}

// --- NewTraceSession: artifacts_manifest.jsonl writer fails (tests defer cleanup of toolsWriter) ---

func TestNewTraceSession_ArtifactsWriterFails(t *testing.T) {
	traceDir := t.TempDir()

	predictedRunID := predictNextRunID(t)
	runDir := filepath.Join(traceDir, predictedRunID)

	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("failed to pre-create run dir: %v", err)
	}

	// Create artifacts_manifest.jsonl as a directory
	obstacle := filepath.Join(runDir, "artifacts_manifest.jsonl")
	if err := os.Mkdir(obstacle, 0o755); err != nil {
		t.Fatalf("failed to create obstacle: %v", err)
	}

	_, err := NewTraceSession(traceDir, "provider", "model")
	if err == nil {
		t.Fatal("expected error when artifacts_manifest.jsonl is a directory, got nil")
	}
	if !strings.Contains(err.Error(), "is a directory") && !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("expected directory-related error, got: %v", err)
	}
}

// --- NewTraceSession: metadata write fails (runs.jsonl writes fail) ---

func TestNewTraceSession_MetadataWriteFails(t *testing.T) {
	traceDir := t.TempDir()

	predictedRunID := predictNextRunID(t)
	runDir := filepath.Join(traceDir, predictedRunID)

	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatalf("failed to pre-create run dir: %v", err)
	}

	// Create runs.jsonl as a symlink to /dev/full.
	// newJSONLWriter opens with O_CREATE|O_WRONLY|O_TRUNC which succeeds on /dev/full,
	// but encoder.Encode (json.NewEncoder.Write) fails with ENOSPC.
	runsTarget := filepath.Join(runDir, "runs.jsonl")
	if err := os.Symlink("/dev/full", runsTarget); err != nil {
		t.Skipf("skipping: cannot create symlink to /dev/full: %v", err)
	}

	_, err := NewTraceSession(traceDir, "provider", "model")
	if err == nil {
		t.Fatal("expected error when metadata write to /dev/full fails (ENOSPC)")
	}
	if !strings.Contains(err.Error(), "failed to write run metadata") {
		t.Errorf("expected 'failed to write run metadata' error, got: %v", err)
	}
	t.Logf("got expected metadata write failure: %v", err)
}

// --- Close: only ToolsFile close produces an error ---

func TestClose_ToolsFileError(t *testing.T) {
	traceDir := t.TempDir()
	s, err := NewTraceSession(traceDir, "anthropic", "claude-3")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Pre-close ONLY the tools file handle to trigger error in Close
	s.mu.Lock()
	if s.ToolsFile != nil {
		s.ToolsFile.file.Close()
	}
	s.mu.Unlock()

	err = s.Close()
	if err == nil {
		t.Log("Close returned nil even though ToolsFile was pre-closed (OS-dependent)")
	} else {
		t.Logf("got expected close error: %v", err)
	}

	// Second close should be idempotent
	if err := s.Close(); err != nil {
		t.Errorf("second Close should be idempotent, got error: %v", err)
	}
}

// --- Close: only ArtifactsFile close produces an error ---

func TestClose_ArtifactsFileError(t *testing.T) {
	traceDir := t.TempDir()
	s, err := NewTraceSession(traceDir, "anthropic", "claude-3")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Pre-close ONLY the artifacts file handle
	s.mu.Lock()
	if s.ArtifactsFile != nil {
		s.ArtifactsFile.file.Close()
	}
	s.mu.Unlock()

	err = s.Close()
	if err == nil {
		t.Log("Close returned nil even though ArtifactsFile was pre-closed (OS-dependent)")
	} else {
		t.Logf("got expected close error: %v", err)
	}

	// Second close should be idempotent
	if err := s.Close(); err != nil {
		t.Errorf("second Close should be idempotent, got error: %v", err)
	}
}

// --- Close: multiple file close errors (returns first error) ---

func TestClose_MultipleFileErrors(t *testing.T) {
	traceDir := t.TempDir()
	s, err := NewTraceSession(traceDir, "anthropic", "claude-3")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	s.mu.Lock()
	// Pre-close runs + tools handles (multiple errors)
	if s.RunsFile != nil {
		s.RunsFile.file.Close()
	}
	if s.ToolsFile != nil {
		s.ToolsFile.file.Close()
	}
	if s.ArtifactsFile != nil {
		s.ArtifactsFile.file.Close()
	}
	s.mu.Unlock()

	// Should return first error, not panic
	err = s.Close()
	if err == nil {
		t.Log("Close returned nil with multiple pre-closed files (OS-dependent)")
	}

	if err := s.Close(); err != nil {
		t.Errorf("second Close should be idempotent, got error: %v", err)
	}
}

// --- RecordTurn: large data with many tool calls ---

func TestRecordTurn_LargeData(t *testing.T) {
	traceDir := t.TempDir()
	s, err := NewTraceSession(traceDir, "anthropic", "claude-3")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	defer s.Close()

	// Build a large system prompt
	largePrompt := strings.Repeat("This is a system prompt line with various instructions. ", 500)

	// Build many messages
	messages := make([]api.Message, 100)
	for i := range messages {
		messages[i] = api.Message{
			Role:    "user",
			Content: strings.Repeat("x", 1000),
		}
	}

	// Build many tool calls
	toolCalls := make([]api.ToolCall, 50)
	for i := range toolCalls {
		toolCalls[i] = api.ToolCall{
			ID:   "tc_" + strings.Repeat("0", i),
			Type: "function",
		}
		toolCalls[i].Function.Name = "tool_" + strings.Repeat("a", i%20)
		toolCalls[i].Function.Arguments = `{"arg":"` + strings.Repeat("d", 200) + `"}`
	}

	record := TurnRecord{
		RunID:            s.GetRunID(),
		TurnIndex:        0,
		SystemPrompt:     largePrompt,
		UserPrompt:       strings.Repeat("User prompt text. ", 200),
		UserPromptOriginal: strings.Repeat("Original user prompt. ", 200),
		MessagesSent:      messages,
		ToolSchemaPayload: json.RawMessage(`[{"type":"function"}]`),
		RawResponse:       strings.Repeat("response body ", 1000),
		ParsedToolCalls:   toolCalls,
		ParserErrors:      []string{"error1", "error2", "error3"},
		FallbackUsed:      true,
		FallbackOutput:    strings.Repeat("fallback ", 500),
		MachineLabels:     []string{"label1", "label2", "label3", "label4"},
		Timestamp:         "2024-06-15T10:30:00Z",
	}

	if err := s.RecordTurn(record); err != nil {
		t.Fatalf("RecordTurn with large data returned error: %v", err)
	}

	s.Close()

	// Read back and verify turn record count
	data, err := os.ReadFile(filepath.Join(s.GetRunDir(), "turns.jsonl"))
	if err != nil {
		t.Fatalf("failed to read turns.jsonl: %v", err)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 10*1024*1024), 10*1024*1024) // 10MB buffer for large JSON lines
	count := 0
	for scanner.Scan() {
		count++
	}
	if count != 1 {
		t.Fatalf("expected 1 turn record, got %d", count)
	}
}

// --- RecordToolCall: all fields populated including ArgsNormalized, ErrorCategory, ErrorMessage ---

func TestRecordToolCall_AllFields(t *testing.T) {
	traceDir := t.TempDir()
	s, err := NewTraceSession(traceDir, "anthropic", "claude-3")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	defer s.Close()

	record := ToolCallRecord{
		RunID:          s.GetRunID(),
		TurnIndex:      3,
		ToolIndex:      7,
		ToolName:       "edit_file",
		Args: map[string]interface{}{
			"path":    "/src/main.go",
			"content": "package main\n\nfunc main() {}",
			"append":  false,
		},
		ArgsNormalized: map[string]interface{}{
			"path":    "/src/main.go",
			"content": "package main\n\nfunc main() {}",
			"append":  false,
		},
		Success:      false,
		FullResult:   strings.Repeat("error line\n", 100),
		ModelResult:  "truncated error output",
		ErrorCategory: "timeout",
		ErrorMessage: "operation timed out after 30s",
		MachineLabels: []string{
			LabelToolCallTimeout,
			LabelToolCallExecutionError,
		},
		Timestamp: "2024-06-15T10:31:00Z",
	}

	if err := s.RecordToolCall(record); err != nil {
		t.Fatalf("RecordToolCall returned error: %v", err)
	}
	s.Close()

	data, err := os.ReadFile(filepath.Join(s.GetRunDir(), "tool_calls.jsonl"))
	if err != nil {
		t.Fatalf("failed to read tool_calls.jsonl: %v", err)
	}

	var got ToolCallRecord
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	if !scanner.Scan() {
		t.Fatal("no line found in tool_calls.jsonl")
	}
	if err := json.Unmarshal(scanner.Bytes(), &got); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if got.ToolName != "edit_file" {
		t.Errorf("ToolName: got %q, want %q", got.ToolName, "edit_file")
	}
	if got.TurnIndex != 3 {
		t.Errorf("TurnIndex: got %d, want 3", got.TurnIndex)
	}
	if got.ToolIndex != 7 {
		t.Errorf("ToolIndex: got %d, want 7", got.ToolIndex)
	}
	if got.ArgsNormalized == nil {
		t.Error("ArgsNormalized should not be nil")
	}
	if got.ErrorCategory != "timeout" {
		t.Errorf("ErrorCategory: got %q, want %q", got.ErrorCategory, "timeout")
	}
	if got.ErrorMessage != "operation timed out after 30s" {
		t.Errorf("ErrorMessage: got %q, want %q", got.ErrorMessage, "operation timed out after 30s")
	}
	if got.Success {
		t.Error("expected Success=false")
	}
	if len(got.MachineLabels) != 2 {
		t.Errorf("MachineLabels: expected 2, got %d", len(got.MachineLabels))
	}
}

// --- RecordArtifact: various artifact types ---

func TestRecordArtifact_VariousTypes(t *testing.T) {
	traceDir := t.TempDir()
	s, err := NewTraceSession(traceDir, "anthropic", "claude-3")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	defer s.Close()

	artifactTypes := []struct {
		artType string
		labels  []string
	}{
		{"file_edit", []string{LabelPathViolationAbsolute}},
		{"file_create", []string{LabelPathViolationNested}},
		{"file_delete", []string{}},
		{"directory_create", []string{LabelPathViolationDisallowed}},
		{"directory_delete", []string{}},
		{"file_read", []string{LabelSchemaEnvelopeViolation, LabelLayoutViolation}},
		{"shell_output", []string{LabelToolCallUnknownTool}},
		{"diff_output", []string{}},
	}

	for i, tt := range artifactTypes {
		record := ArtifactManifest{
			RunID:        s.GetRunID(),
			RelativePath: "path/to/artifact_" + tt.artType,
			SizeBytes:    int64(1024 * (i + 1)),
			Hash:         strings.Repeat(string('a'+byte(i)), 64),
			ArtifactType: tt.artType,
			MachineLabels: tt.labels,
			Timestamp:    "2024-06-15T10:32:00Z",
		}

		if err := s.RecordArtifact(record); err != nil {
			t.Fatalf("RecordArtifact %d (%s) error: %v", i, tt.artType, err)
		}
	}

	s.Close()

	// Read back and verify count
	data, err := os.ReadFile(filepath.Join(s.GetRunDir(), "artifacts_manifest.jsonl"))
	if err != nil {
		t.Fatalf("failed to read artifacts_manifest.jsonl: %v", err)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	count := 0
	for scanner.Scan() {
		var art ArtifactManifest
		if err := json.Unmarshal(scanner.Bytes(), &art); err != nil {
			t.Fatalf("failed to unmarshal artifact at line %d: %v", count+1, err)
		}
		if art.ArtifactType != artifactTypes[count].artType {
			t.Errorf("line %d: ArtifactType got %q, want %q", count+1, art.ArtifactType, artifactTypes[count].artType)
		}
		if len(art.MachineLabels) != len(artifactTypes[count].labels) {
			t.Errorf("line %d: MachineLabels count got %d, want %d", count+1, len(art.MachineLabels), len(artifactTypes[count].labels))
		}
		count++
	}

	if count != len(artifactTypes) {
		t.Errorf("expected %d artifact records, got %d", len(artifactTypes), count)
	}
}

// --- Concurrent RecordTurn and RecordToolCall (non-flaky) ---

func TestConcurrent_RecordTurnAndToolCall(t *testing.T) {
	traceDir := t.TempDir()
	s, err := NewTraceSession(traceDir, "anthropic", "claude-3")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	const numGoroutines = 10
	const perGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines * 2) // half turns, half tool calls

	// Launch turn writers
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				err := s.RecordTurn(TurnRecord{
					RunID:     s.GetRunID(),
					TurnIndex: id*perGoroutine + j,
					Timestamp: "2024-01-01T00:00:00Z",
				})
				if err != nil {
					t.Errorf("goroutine %d turn %d: %v", id, j, err)
				}
			}
		}(i)
	}

	// Launch tool call writers
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				err := s.RecordToolCall(ToolCallRecord{
					RunID:     s.GetRunID(),
					TurnIndex: id,
					ToolIndex: j,
					ToolName:  "concurrent_tool",
					Args:      map[string]interface{}{"id": id, "idx": j},
					Timestamp: "2024-01-01T00:00:00Z",
				})
				if err != nil {
					t.Errorf("goroutine %d tool %d: %v", id, j, err)
				}
			}
		}(i)
	}

	wg.Wait()

	if err := s.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	// Verify correct line counts
	for _, file := range []struct {
		name     string
		expected int
	}{
		{"turns.jsonl", numGoroutines * perGoroutine},
		{"tool_calls.jsonl", numGoroutines * perGoroutine},
	} {
		data, err := os.ReadFile(filepath.Join(s.GetRunDir(), file.name))
		if err != nil {
			t.Fatalf("failed to read %s: %v", file.name, err)
		}
		lines := 0
		for _, b := range data {
			if b == '\n' {
				lines++
			}
		}
		if lines != file.expected {
			t.Errorf("%s: expected %d lines, got %d", file.name, file.expected, lines)
		}
	}
}
