package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- WriteDiagnosticTranscript ---

func TestToolThreading_WriteDiagnosticTranscript_CreatesDirAndFile(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "nested", "diagnostics")

	c := DiagnosticCapture{
		Trigger:  DiagnosticTriggerProviderRejection,
		Provider: "minimax",
		Messages: []Message{{Role: "user", Content: "hello"}},
	}

	path, err := WriteDiagnosticTranscript(c, subDir)
	if err != nil {
		t.Fatalf("WriteDiagnosticTranscript returned error: %v", err)
	}

	// Verify the file exists
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("file does not exist at %s: %v", path, err)
	}
	if info.IsDir() {
		t.Error("expected a file, got a directory")
	}
}

func TestToolThreading_WriteDiagnosticTranscript_ValidJSON(t *testing.T) {
	dir := t.TempDir()

	c := DiagnosticCapture{
		Trigger:   DiagnosticTriggerPreSendValidation,
		Provider:  "deepseek",
		Iteration: 42,
		Violations: []ToolThreadingViolation{
			{Kind: "out_of_order", Index: 3, ToolCallID: "call-abc", Detail: "reversed"},
		},
		Messages: []Message{
			{Role: "user", Content: "read file"},
			{Role: "assistant", Content: "", ToolCalls: []ToolCall{
				{ID: "call-abc", Function: ToolCallFunction{Name: "read_file", Arguments: `{}`}},
			}},
			{Role: "tool", Content: "contents", ToolCallID: "call-abc"},
		},
		Error:        "HTTP 400: tool call result does not follow tool call (2013)",
		SystemPrompt: "You are a helpful assistant.",
	}

	path, err := WriteDiagnosticTranscript(c, dir)
	if err != nil {
		t.Fatalf("WriteDiagnosticTranscript returned error: %v", err)
	}

	// Read and unmarshal the JSON
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	var captured DiagnosticCapture
	if err := json.Unmarshal(data, &captured); err != nil {
		t.Fatalf("written JSON is not valid: %v", err)
	}

	// Verify fields round-trip correctly
	if captured.Trigger != DiagnosticTriggerPreSendValidation {
		t.Errorf("Trigger = %q, want %q", captured.Trigger, DiagnosticTriggerPreSendValidation)
	}
	if captured.Provider != "deepseek" {
		t.Errorf("Provider = %q, want %q", captured.Provider, "deepseek")
	}
	if captured.Iteration != 42 {
		t.Errorf("Iteration = %d, want 42", captured.Iteration)
	}
	if len(captured.Violations) != 1 {
		t.Fatalf("Violations length = %d, want 1", len(captured.Violations))
	}
	if captured.Violations[0].Kind != "out_of_order" {
		t.Errorf("Violation kind = %q, want %q", captured.Violations[0].Kind, "out_of_order")
	}
	if captured.Violations[0].Index != 3 {
		t.Errorf("Violation index = %d, want 3", captured.Violations[0].Index)
	}
	if captured.Violations[0].ToolCallID != "call-abc" {
		t.Errorf("Violation ToolCallID = %q, want %q", captured.Violations[0].ToolCallID, "call-abc")
	}
	if len(captured.Messages) != 3 {
		t.Fatalf("Messages length = %d, want 3", len(captured.Messages))
	}
	if captured.Messages[0].Role != "user" {
		t.Errorf("First message role = %q, want %q", captured.Messages[0].Role, "user")
	}
	if captured.Error != "HTTP 400: tool call result does not follow tool call (2013)" {
		t.Errorf("Error = %q, want expected error string", captured.Error)
	}
	if captured.SystemPrompt != "You are a helpful assistant." {
		t.Errorf("SystemPrompt = %q, want expected prompt", captured.SystemPrompt)
	}
}

func TestToolThreading_WriteDiagnosticTranscript_FilenameContainsTrigger(t *testing.T) {
	dir := t.TempDir()

	c := DiagnosticCapture{
		Trigger:  DiagnosticTriggerProviderRejection,
		Messages: []Message{},
	}

	path, err := WriteDiagnosticTranscript(c, dir)
	if err != nil {
		t.Fatalf("WriteDiagnosticTranscript returned error: %v", err)
	}

	base := filepath.Base(path)

	// Filename should contain the trigger string
	if !strings.Contains(base, "provider_threading_rejection") {
		t.Errorf("filename %q should contain trigger string %q", base, DiagnosticTriggerProviderRejection)
	}

	// Should have .json extension
	if !strings.HasSuffix(base, ".json") {
		t.Errorf("filename %q should end with .json", base)
	}
}

func TestToolThreading_WriteDiagnosticTranscript_FilenameContainsThreading(t *testing.T) {
	dir := t.TempDir()

	c := DiagnosticCapture{
		Trigger:  DiagnosticTriggerPreSendValidation,
		Messages: []Message{},
	}

	path, err := WriteDiagnosticTranscript(c, dir)
	if err != nil {
		t.Fatalf("WriteDiagnosticTranscript returned error: %v", err)
	}

	base := filepath.Base(path)

	if !strings.Contains(base, "threading") {
		t.Errorf("filename %q should contain 'threading'", base)
	}
}

func TestToolThreading_WriteDiagnosticTranscript_EmptyMessagesPreserved(t *testing.T) {
	dir := t.TempDir()

	// Explicitly empty slice (not nil)
	c := DiagnosticCapture{
		Trigger:  DiagnosticTriggerProviderRejection,
		Messages: []Message{},
	}

	path, err := WriteDiagnosticTranscript(c, dir)
	if err != nil {
		t.Fatalf("WriteDiagnosticTranscript returned error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	var captured DiagnosticCapture
	if err := json.Unmarshal(data, &captured); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// Empty slice should round-trip as empty slice (not nil)
	if captured.Messages == nil {
		t.Error("empty Messages slice became nil after round-trip")
	}
	if len(captured.Messages) != 0 {
		t.Errorf("Messages length = %d, want 0", len(captured.Messages))
	}
}

func TestToolThreading_WriteDiagnosticTranscript_ReturnsPathThatExists(t *testing.T) {
	dir := t.TempDir()

	c := DiagnosticCapture{
		Trigger:  DiagnosticTriggerProviderRejection,
		Provider: "minimax",
		Messages: []Message{
			{Role: "user", Content: "test"},
			{Role: "assistant", Content: "ok"},
		},
	}

	path, err := WriteDiagnosticTranscript(c, dir)
	if err != nil {
		t.Fatalf("WriteDiagnosticTranscript returned error: %v", err)
	}

	// The returned path should be under the given dir
	if !strings.HasPrefix(path, dir) {
		t.Errorf("path %q should be under dir %q", path, dir)
	}

	// File should be readable
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("returned path is not readable: %v", err)
	}
	if len(data) == 0 {
		t.Error("written file is empty")
	}
}

func TestToolThreading_WriteDiagnosticTranscript_MultipleWrites(t *testing.T) {
	dir := t.TempDir()

	// Write two captures
	c1 := DiagnosticCapture{
		Trigger:  DiagnosticTriggerProviderRejection,
		Provider: "minimax",
		Messages: []Message{{Role: "user", Content: "first"}},
	}
	path1, err := WriteDiagnosticTranscript(c1, dir)
	if err != nil {
		t.Fatalf("first write failed: %v", err)
	}

	c2 := DiagnosticCapture{
		Trigger:  DiagnosticTriggerPreSendValidation,
		Provider: "deepseek",
		Messages: []Message{{Role: "user", Content: "second"}},
	}
	path2, err := WriteDiagnosticTranscript(c2, dir)
	if err != nil {
		t.Fatalf("second write failed: %v", err)
	}

	// Both files should exist independently
	if _, err := os.Stat(path1); err != nil {
		t.Errorf("first file missing after second write: %v", err)
	}
	if _, err := os.Stat(path2); err != nil {
		t.Errorf("second file missing: %v", err)
	}
}
