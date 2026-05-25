package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// NewAuditLogger
// ---------------------------------------------------------------------------

func TestNewAuditLogger(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "audit.log")

	logger, err := NewAuditLogger(logPath)
	if err != nil {
		t.Fatalf("NewAuditLogger() returned error: %v", err)
	}
	if logger == nil {
		t.Fatal("NewAuditLogger() returned nil logger")
	}

	// Verify the file was created.
	_, err = os.Stat(logPath)
	if err != nil {
		t.Fatalf("log file was not created: %v", err)
	}

	// Clean up.
	err = logger.Close()
	if err != nil {
		t.Errorf("Close() returned error: %v", err)
	}
}

func TestNewAuditLogger_CreatesParentDirs(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "a", "b", "c", "audit.log")

	logger, err := NewAuditLogger(logPath)
	if err != nil {
		t.Fatalf("NewAuditLogger() with nested path returned error: %v", err)
	}
	if logger == nil {
		t.Fatal("NewAuditLogger() with nested path returned nil logger")
	}

	// Verify the full directory tree was created.
	_, err = os.Stat(filepath.Join(tmpDir, "a", "b", "c"))
	if err != nil {
		t.Fatalf("parent directories were not created: %v", err)
	}

	// Verify the file was created at the full path.
	_, err = os.Stat(logPath)
	if err != nil {
		t.Fatalf("log file was not created at nested path: %v", err)
	}

	logger.Close()
}

// ---------------------------------------------------------------------------
// Log — JSONL output
// ---------------------------------------------------------------------------

func TestLog_WritesJSONL(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "audit.jsonl")

	logger, err := NewAuditLogger(logPath)
	if err != nil {
		t.Fatalf("NewAuditLogger() returned error: %v", err)
	}
	defer logger.Close()

	// Write 3 entries with different tools, risk levels, and actions.
	entries := []AuditEntry{
		{Timestamp: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC), Tool: "ls", RiskLevel: "SAFE", Category: "read-only", Action: "allowed"},
		{Timestamp: time.Date(2024, 1, 15, 10, 1, 0, 0, time.UTC), Tool: "rm -rf /", RiskLevel: "DANGEROUS", Category: "destructive", Action: "denied"},
		{Timestamp: time.Date(2024, 1, 15, 10, 2, 0, 0, time.UTC), Tool: "sudo apt install", RiskLevel: "CAUTION", Category: "privileged", Action: "prompted"},
	}

	for _, entry := range entries {
		if err := logger.Log(entry); err != nil {
			t.Fatalf("Log(%+v) returned error: %v", entry, err)
		}
	}

	// Close before reading back to ensure all data is flushed.
	logger.Close()

	// Read the file and verify line count.
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("os.ReadFile() returned error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}

	// Parse each line and verify fields match.
	for i, line := range lines {
		var parsed AuditEntry
		if err := json.Unmarshal([]byte(line), &parsed); err != nil {
			t.Fatalf("failed to parse line %d as JSON: %v\nLine: %s", i+1, err, line)
		}

		expected := entries[i]
		if parsed.Tool != expected.Tool {
			t.Errorf("line %d: Tool = %q, want %q", i+1, parsed.Tool, expected.Tool)
		}
		if parsed.RiskLevel != expected.RiskLevel {
			t.Errorf("line %d: RiskLevel = %q, want %q", i+1, parsed.RiskLevel, expected.RiskLevel)
		}
		if parsed.Category != expected.Category {
			t.Errorf("line %d: Category = %q, want %q", i+1, parsed.Category, expected.Category)
		}
		if parsed.Action != expected.Action {
			t.Errorf("line %d: Action = %q, want %q", i+1, parsed.Action, expected.Action)
		}
	}
}

// ---------------------------------------------------------------------------
// Log — Entry format
// ---------------------------------------------------------------------------

func TestLog_EntryFormat(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "audit.jsonl")

	logger, err := NewAuditLogger(logPath)
	if err != nil {
		t.Fatalf("NewAuditLogger() returned error: %v", err)
	}
	defer logger.Close()

	ts := time.Date(2024, 6, 15, 14, 30, 0, 0, time.UTC)
	entry := AuditEntry{
		Timestamp: ts,
		Tool:      "shell_command",
		Args:      `{"command":"ls -la /tmp"}`,
		RiskLevel: "SAFE",
		Category:  "read-only",
		Action:    "allowed",
		Reasoning: "Read-only shell command in workspace",
		Source:    "classifier",
		SessionID: "session-abc-123",
		Workspace: "/home/user/project",
	}

	if err := logger.Log(entry); err != nil {
		t.Fatalf("Log() returned error: %v", err)
	}

	logger.Close()

	// Read back and parse.
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("os.ReadFile() returned error: %v", err)
	}

	var parsed AuditEntry
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &parsed); err != nil {
		t.Fatalf("failed to parse entry: %v", err)
	}

	// Verify all fields are present.
	if parsed.Tool != entry.Tool {
		t.Errorf("Tool = %q, want %q", parsed.Tool, entry.Tool)
	}
	if parsed.Args != entry.Args {
		t.Errorf("Args = %q, want %q", parsed.Args, entry.Args)
	}
	if parsed.RiskLevel != entry.RiskLevel {
		t.Errorf("RiskLevel = %q, want %q", parsed.RiskLevel, entry.RiskLevel)
	}
	if parsed.Category != entry.Category {
		t.Errorf("Category = %q, want %q", parsed.Category, entry.Category)
	}
	if parsed.Action != entry.Action {
		t.Errorf("Action = %q, want %q", parsed.Action, entry.Action)
	}
	if parsed.Reasoning != entry.Reasoning {
		t.Errorf("Reasoning = %q, want %q", parsed.Reasoning, entry.Reasoning)
	}
	if parsed.Source != entry.Source {
		t.Errorf("Source = %q, want %q", parsed.Source, entry.Source)
	}
	if parsed.SessionID != entry.SessionID {
		t.Errorf("SessionID = %q, want %q", parsed.SessionID, entry.SessionID)
	}
	if parsed.Workspace != entry.Workspace {
		t.Errorf("Workspace = %q, want %q", parsed.Workspace, entry.Workspace)
	}

	// Verify timestamp is a valid RFC3339 format by parsing the raw JSON.
	var rawMap map[string]string
	if err := json.Unmarshal(data, &rawMap); err != nil {
		t.Fatalf("failed to parse raw JSON for timestamp check: %v", err)
	}
	tsStr := rawMap["timestamp"]
	if tsStr == "" {
		t.Error("timestamp field is missing from JSON output")
	} else {
		parsedTime, err := time.Parse(time.RFC3339, tsStr)
		if err != nil {
			t.Errorf("timestamp %q is not valid RFC3339: %v", tsStr, err)
		} else if !parsedTime.Equal(ts) {
			t.Errorf("parsed timestamp %v does not match original %v", parsedTime, ts)
		}
	}
}

// ---------------------------------------------------------------------------
// Log — omitempty fields
// ---------------------------------------------------------------------------

func TestLog_OmitsEmptyFields(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "audit.jsonl")

	logger, err := NewAuditLogger(logPath)
	if err != nil {
		t.Fatalf("NewAuditLogger() returned error: %v", err)
	}
	defer logger.Close()

	// Write an entry with only required fields (no omitempty fields set).
	entry := AuditEntry{
		Timestamp: time.Now(),
		Tool:      "ls",
		RiskLevel: "SAFE",
		Category:  "read-only",
		Action:    "allowed",
		// Args, Reasoning, Source, SessionID, Workspace are all empty — should be omitted.
	}

	if err := logger.Log(entry); err != nil {
		t.Fatalf("Log() returned error: %v", err)
	}

	logger.Close()

	// Read back the raw JSON line.
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("os.ReadFile() returned error: %v", err)
	}

	line := strings.TrimSpace(string(data))

	// The omitempty fields should NOT appear in the JSON output.
	omitemptyFields := []string{"args", "reasoning", "source", "session_id", "workspace"}
	for _, field := range omitemptyFields {
		// Check for the field name in JSON (e.g., "args":)
		if strings.Contains(line, `"`+field+`"`) {
			t.Errorf("JSON should not contain omitempty field %q, but found it in: %s", field, line)
		}
	}

	// Verify the required fields ARE present.
	requiredFields := []string{"timestamp", "tool", "risk_level", "category", "action"}
	for _, field := range requiredFields {
		if !strings.Contains(line, `"`+field+`"`) {
			t.Errorf("JSON should contain required field %q, but it is missing from: %s", field, line)
		}
	}
}

// ---------------------------------------------------------------------------
// Log — Concurrent writes
// ---------------------------------------------------------------------------

func TestLog_ConcurrentWrites(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "audit.jsonl")

	logger, err := NewAuditLogger(logPath)
	if err != nil {
		t.Fatalf("NewAuditLogger() returned error: %v", err)
	}
	defer logger.Close()

	const numGoroutines = 60

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(i int) {
			defer wg.Done()
			toolName := "tool-" + strings.Repeat("x", i) // unique-ish name per goroutine
			entry := AuditEntry{
				Timestamp: time.Now(),
				Tool:      toolName,
				RiskLevel: "SAFE",
				Category:  "read-only",
				Action:    "allowed",
			}
			if err := logger.Log(entry); err != nil {
				t.Errorf("Log() goroutine %d returned error: %v", i, err)
			}
		}(i)
	}

	wg.Wait()
	logger.Close()

	// Read back and verify line count.
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("os.ReadFile() returned error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != numGoroutines {
		t.Fatalf("expected %d lines, got %d (data loss or duplication from concurrent writes)", numGoroutines, len(lines))
	}

	// Verify each line is valid JSON.
	toolNames := make(map[string]bool, numGoroutines)
	for i, line := range lines {
		var parsed AuditEntry
		if err := json.Unmarshal([]byte(line), &parsed); err != nil {
			t.Errorf("line %d is not valid JSON: %v\nLine: %s", i+1, err, line)
		} else {
			toolNames[parsed.Tool] = true
		}
	}

	// Verify all tool names are present (no data loss).
	if len(toolNames) != numGoroutines {
		t.Errorf("expected %d unique tool names, got %d — some entries may have been lost", numGoroutines, len(toolNames))
	}
}

// ---------------------------------------------------------------------------
// Nil-receiver safety
// ---------------------------------------------------------------------------

func TestNilLogger_LogEntry(t *testing.T) {
	t.Parallel()

	var l *AuditLogger // nil

	err := l.LogEntry(AuditEntry{Tool: "ls", RiskLevel: "SAFE"})
	if err != nil {
		t.Errorf("nil.LogEntry() returned error: %v (expected nil)", err)
	}
}

func TestNilLogger_Log(t *testing.T) {
	t.Parallel()

	var l *AuditLogger // nil

	err := l.Log(AuditEntry{Tool: "ls", RiskLevel: "SAFE"})
	if err != nil {
		t.Errorf("nil.Log() returned error: %v (expected nil)", err)
	}
}

func TestNilLogger_Close(t *testing.T) {
	t.Parallel()

	var l *AuditLogger // nil

	err := l.Close()
	if err != nil {
		t.Errorf("nil.Close() returned error: %v (expected nil)", err)
	}
}

// ---------------------------------------------------------------------------
// Close
// ---------------------------------------------------------------------------

func TestClose(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "audit.jsonl")

	logger, err := NewAuditLogger(logPath)
	if err != nil {
		t.Fatalf("NewAuditLogger() returned error: %v", err)
	}

	// Write an entry before closing.
	if err := logger.Log(AuditEntry{
		Timestamp: time.Now(),
		Tool:      "ls",
		RiskLevel: "SAFE",
		Action:    "allowed",
	}); err != nil {
		t.Fatalf("Log() before close returned error: %v", err)
	}

	// Close should succeed.
	if err := logger.Close(); err != nil {
		t.Errorf("Close() returned error: %v", err)
	}

	// Writing after close should fail.
	err = logger.Log(AuditEntry{
		Timestamp: time.Now(),
		Tool:      "cat",
		RiskLevel: "SAFE",
		Action:    "allowed",
	})
	if err == nil {
		t.Error("Log() after Close() should return an error, but got nil")
	}
}

// ---------------------------------------------------------------------------
// SetAuditLogger integration with ClassifyToolCall
// ---------------------------------------------------------------------------

func TestClosedLogger_LogEntry(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	logPath := filepath.Join(dir, "audit.log")

	logger, err := NewAuditLogger(logPath)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}

	// Close the logger
	if err := logger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// LogEntry after close should return an error
	err = logger.LogEntry(AuditEntry{
		Timestamp: time.Now(),
		Tool:      "test_tool",
		RiskLevel: "safe",
		Action:    "allowed",
	})
	if err == nil {
		t.Fatal("expected error when calling LogEntry after Close, got nil")
	}
}

func TestSetAuditLogger_Integration(t *testing.T) {
	// Cannot run in parallel — SetAuditLogger uses a package-level variable
	// shared across all tests.

	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "audit.jsonl")

	logger, err := NewAuditLogger(logPath)
	if err != nil {
		t.Fatalf("NewAuditLogger() returned error: %v", err)
	}

	// Set the package-level audit logger and ensure cleanup.
	SetAuditLogger(logger)
	defer func() {
		logger.Close()
		SetAuditLogger(nil) // reset to avoid affecting other tests
	}()

	// Classify several different tool calls to generate audit entries.
	calls := []struct {
		toolName string
		args     map[string]interface{}
	}{
		{"shell_command", map[string]interface{}{"command": "ls -la"}},
		{"shell_command", map[string]interface{}{"command": "rm -rf /"}},
		{"write_file", map[string]interface{}{"path": "src/main.go", "content": "hello"}},
		{"git", map[string]interface{}{"operation": "commit"}},
		{"fetch_url", map[string]interface{}{"url": "https://example.com"}},
		{"unknown_tool", map[string]interface{}{}},
	}

	for _, call := range calls {
		ClassifyToolCall(call.toolName, call.args)
	}

	// Close the logger to flush all entries before reading.
	logger.Close()

	// Read back the file.
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("os.ReadFile() returned error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	expectedCount := len(calls)
	if len(lines) != expectedCount {
		t.Fatalf("expected %d audit log entries, got %d", expectedCount, len(lines))
	}

	// Verify each line is valid JSON and contains the expected tool name.
	for i, line := range lines {
		var parsed AuditEntry
		if err := json.Unmarshal([]byte(line), &parsed); err != nil {
			t.Errorf("line %d is not valid JSON: %v", i+1, err)
			continue
		}

		expected := calls[i]
		if parsed.Tool != expected.toolName {
			t.Errorf("line %d: Tool = %q, want %q", i+1, parsed.Tool, expected.toolName)
		}

		// Verify common fields are non-empty for valid entries.
		if parsed.RiskLevel == "" {
			t.Errorf("line %d: RiskLevel is empty", i+1)
		}
		if parsed.Action == "" {
			t.Errorf("line %d: Action is empty", i+1)
		}
		if parsed.Source != "classifier" {
			t.Errorf("line %d: Source = %q, want %q", i+1, parsed.Source, "classifier")
		}
	}
}
