package agent

import (
	"strings"
	"testing"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/history"
)

func TestDetermineWriteOperation_Create(t *testing.T) {
	op := determineWriteOperation("", "new content")
	if op != "create" {
		t.Errorf("expected 'create', got '%s'", op)
	}
}

func TestDetermineWriteOperation_Write(t *testing.T) {
	op := determineWriteOperation("original", "different")
	if op != "write" {
		t.Errorf("expected 'write', got '%s'", op)
	}
}

func TestDetermineWriteOperation_Overwrite(t *testing.T) {
	content := "same content"
	op := determineWriteOperation(content, content)
	if op != "overwrite" {
		t.Errorf("expected 'overwrite', got '%s'", op)
	}
}

func TestDetermineWriteOperation_BothEmpty(t *testing.T) {
	op := determineWriteOperation("", "")
	if op != "create" {
		t.Errorf("expected 'create' for both empty, got '%s'", op)
	}
}

func TestLimitString_ShortString(t *testing.T) {
	result := limitString("hi", 10)
	if result != "hi" {
		t.Errorf("expected 'hi', got '%s'", result)
	}
}

func TestLimitString_ExactLength(t *testing.T) {
	s := "exact12"
	result := limitString(s, 7)
	if result != s {
		t.Errorf("expected '%s', got '%s'", s, result)
	}
}

func TestLimitString_Truncated(t *testing.T) {
	result := limitString("this is a longer string", 10)
	expected := "this is a ..."
	if result != expected {
		t.Errorf("expected '%s', got '%s'", expected, result)
	}
}

func TestLimitString_ZeroMax(t *testing.T) {
	result := limitString("anything", 0)
	expected := "..."
	if result != expected {
		t.Errorf("expected '%s', got '%s'", expected, result)
	}
}

func TestLimitString_EmptyString(t *testing.T) {
	result := limitString("", 5)
	if result != "" {
		t.Errorf("expected empty string, got '%s'", result)
	}
}

func TestChangeTracker_EnableDisable(t *testing.T) {
	ct := ChangeTracker{}

	if ct.IsEnabled() {
		t.Error("expected default disabled state to be false")
	}

	ct.Enable()
	if !ct.IsEnabled() {
		t.Error("expected enabled after Enable()")
	}

	ct.Disable()
	if ct.IsEnabled() {
		t.Error("expected disabled after Disable()")
	}

	ct.Enable()
	ct.Enable() // double-enable should still be true
	if !ct.IsEnabled() {
		t.Error("expected still enabled after double Enable()")
	}

	ct.Disable()
	ct.Disable() // double-disable should still be false
	if ct.IsEnabled() {
		t.Error("expected still disabled after double Disable()")
	}
}

func TestChangeTracker_TrackFileEdit(t *testing.T) {
	ct := ChangeTracker{}
	ct.Enable()

	err := ct.TrackFileEdit("test/file.go", "original code", "new code")
	if err != nil {
		t.Fatalf("TrackFileEdit returned error: %v", err)
	}

	if ct.GetChangeCount() != 1 {
		t.Fatalf("expected 1 change, got %d", ct.GetChangeCount())
	}

	changes := ct.GetChanges()
	c := changes[0]

	if c.FilePath != "test/file.go" {
		t.Errorf("expected file path 'test/file.go', got '%s'", c.FilePath)
	}
	if c.OriginalCode != "original code" {
		t.Errorf("expected original 'original code', got '%s'", c.OriginalCode)
	}
	if c.NewCode != "new code" {
		t.Errorf("expected new 'new code', got '%s'", c.NewCode)
	}
	if c.Operation != "edit" {
		t.Errorf("expected operation 'edit', got '%s'", c.Operation)
	}
	if c.ToolCall != "EditFile" {
		t.Errorf("expected tool call 'EditFile', got '%s'", c.ToolCall)
	}
	if c.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
}

func TestChangeTracker_TrackFileWrite_Disabled(t *testing.T) {
	ct := ChangeTracker{}
	// disabled by default (zero value)

	err := ct.TrackFileWrite("nonexistent/path.txt", "content")
	if err != nil {
		t.Fatalf("TrackFileWrite when disabled returned error: %v", err)
	}

	if ct.GetChangeCount() != 0 {
		t.Errorf("expected 0 changes when disabled, got %d", ct.GetChangeCount())
	}
}

func TestChangeTracker_GetTrackedFiles(t *testing.T) {
	ct := ChangeTracker{}
	ct.Enable()

	ct.TrackFileEdit("a.go", "old", "new")
	ct.TrackFileEdit("b.go", "old", "new")
	ct.TrackFileEdit("c.go", "old", "new")

	files := ct.GetTrackedFiles()
	if len(files) != 3 {
		t.Fatalf("expected 3 files, got %d", len(files))
	}

	expected := []string{"a.go", "b.go", "c.go"}
	for i, f := range files {
		if f != expected[i] {
			t.Errorf("file[%d]: expected '%s', got '%s'", i, expected[i], f)
		}
	}
}

func TestChangeTracker_GetChangeCount(t *testing.T) {
	ct := ChangeTracker{}
	ct.Enable()

	if ct.GetChangeCount() != 0 {
		t.Errorf("expected 0 initially, got %d", ct.GetChangeCount())
	}

	ct.TrackFileEdit("f1.go", "a", "b")
	if ct.GetChangeCount() != 1 {
		t.Errorf("expected 1 after first edit, got %d", ct.GetChangeCount())
	}

	ct.TrackFileEdit("f2.go", "c", "d")
	if ct.GetChangeCount() != 2 {
		t.Errorf("expected 2 after second edit, got %d", ct.GetChangeCount())
	}
}

func TestChangeTracker_GetChanges_Independence(t *testing.T) {
	ct := ChangeTracker{}
	ct.Enable()

	ct.TrackFileEdit("file.go", "original", "new")

	// Get the changes slice and modify it
	changes := ct.GetChanges()
	changes[0].FilePath = "tampered.go"
	changes[0].NewCode = "tampered"

	// Verify the tracker's internal state is unchanged
	internal := ct.GetChanges()
	if internal[0].FilePath != "file.go" {
		t.Errorf("tracker was modified externally: got '%s'", internal[0].FilePath)
	}
	if internal[0].NewCode != "new" {
		t.Errorf("tracker was modified externally: got '%s'", internal[0].NewCode)
	}
}

func TestChangeTracker_Clear(t *testing.T) {
	ct := ChangeTracker{}
	ct.Enable()

	ct.TrackFileEdit("f1.go", "a", "b")
	ct.TrackFileEdit("f2.go", "c", "d")

	if ct.GetChangeCount() != 2 {
		t.Fatalf("expected 2 changes before clear, got %d", ct.GetChangeCount())
	}

	ct.Clear()

	if ct.GetChangeCount() != 0 {
		t.Errorf("expected 0 changes after clear, got %d", ct.GetChangeCount())
	}

	files := ct.GetTrackedFiles()
	if len(files) != 0 {
		t.Errorf("expected 0 files after clear, got %d", len(files))
	}

	// Tracker should still be enabled
	if !ct.IsEnabled() {
		t.Error("expected tracker still enabled after Clear()")
	}
}

func TestChangeTracker_Reset(t *testing.T) {
	ct := ChangeTracker{}
	ct.Enable()
	ct.sessionID = "sess-123"
	ct.instructions = "original instructions"

	ct.TrackFileEdit("file.go", "a", "b")

	if ct.GetChangeCount() != 1 {
		t.Fatalf("expected 1 change before reset, got %d", ct.GetChangeCount())
	}

	ct.Reset("new instructions")

	if ct.GetChangeCount() != 0 {
		t.Errorf("expected 0 changes after reset, got %d", ct.GetChangeCount())
	}

	if ct.instructions != "new instructions" {
		t.Errorf("expected instructions 'new instructions', got '%s'", ct.instructions)
	}

	// revisionID should have been regenerated (different from original)
	// We can't easily check the exact value, but verify it's non-empty
	if ct.revisionID == "" {
		t.Error("expected non-empty revisionID after reset")
	}
}

func TestChangeTracker_GetSummary_NoChanges(t *testing.T) {
	ct := ChangeTracker{}

	summary := ct.GetSummary()
	if summary != "No file changes tracked" {
		t.Errorf("expected 'No file changes tracked', got '%s'", summary)
	}
}

func TestChangeTracker_GetSummary_WithChanges(t *testing.T) {
	ct := ChangeTracker{}
	ct.Enable()

	ct.TrackFileEdit("src/main.go", "old", "new")
	ct.changes[0].Operation = "edit"

	// Simulate a write operation
	ct.changes = append(ct.changes, TrackedFileChange{
		FilePath:     "src/utils.go",
		OriginalCode: "",
		NewCode:      "package utils",
		Operation:    "create",
		ToolCall:     "WriteFile",
	})

	summary := ct.GetSummary()

	if !strings.Contains(summary, "Tracked 2 file changes") {
		t.Errorf("expected count in summary, got: %s", summary)
	}
	if !strings.Contains(summary, "src/main.go") {
		t.Errorf("expected 'src/main.go' in summary, got: %s", summary)
	}
	if !strings.Contains(summary, "edit") {
		t.Errorf("expected 'edit' operation in summary, got: %s", summary)
	}
	if !strings.Contains(summary, "src/utils.go") {
		t.Errorf("expected 'src/utils.go' in summary, got: %s", summary)
	}
	if !strings.Contains(summary, "create") {
		t.Errorf("expected 'create' operation in summary, got: %s", summary)
	}
}

func TestChangeTracker_Commit_NoChanges(t *testing.T) {
	// Initialize history paths to avoid errors
	history.InitializeHistoryPaths(nil)

	ct := ChangeTracker{}
	ct.Enable()

	err := ct.Commit("response", nil)
	if err != nil {
		t.Errorf("expected nil when no changes, got: %v", err)
	}
}

func TestChangeTracker_Commit_Disabled(t *testing.T) {
	history.InitializeHistoryPaths(nil)

	ct := ChangeTracker{}
	// disabled by default
	ct.changes = []TrackedFileChange{{
		FilePath:     "file.go",
		OriginalCode: "old",
		NewCode:      "new",
		Operation:    "edit",
		ToolCall:     "EditFile",
	}}

	err := ct.Commit("response", nil)
	if err != nil {
		t.Errorf("expected nil when disabled, got: %v", err)
	}
}

func TestConvertToHistoryMessages_NilInput(t *testing.T) {
	result := convertToHistoryMessages(nil)
	if result != nil {
		t.Errorf("expected nil for nil input, got %v", result)
	}
}

func TestConvertToHistoryMessages_EmptyInput(t *testing.T) {
	result := convertToHistoryMessages([]api.Message{})
	if result == nil {
		t.Error("expected non-nil empty slice for empty input")
	}
	if len(result) != 0 {
		t.Errorf("expected length 0, got %d", len(result))
	}
}

func TestConvertToHistoryMessages_SimpleMessages(t *testing.T) {
	input := []api.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
	}

	result := convertToHistoryMessages(input)
	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}

	if result[0].Role != "user" {
		t.Errorf("expected role 'user', got '%s'", result[0].Role)
	}
	if result[0].Content != "hello" {
		t.Errorf("expected content 'hello', got '%s'", result[0].Content)
	}
	if result[1].Role != "assistant" {
		t.Errorf("expected role 'assistant', got '%s'", result[1].Role)
	}
	if result[1].Content != "hi there" {
		t.Errorf("expected content 'hi there', got '%s'", result[1].Content)
	}
}

func TestConvertToHistoryMessages_WithToolCalls(t *testing.T) {
	input := []api.Message{
		{
			Role:    "assistant",
			Content: "let me do that",
			ToolCalls: []api.ToolCall{
				{
					ID:   "call_1",
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{Name: "write_file", Arguments: "{}"},
				},
			},
		},
	}

	result := convertToHistoryMessages(input)
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}

	msg := result[0]
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(msg.ToolCalls))
	}

	tc := msg.ToolCalls[0]
	if tc.ID != "call_1" {
		t.Errorf("expected tool call ID 'call_1', got '%s'", tc.ID)
	}
	if tc.Type != "function" {
		t.Errorf("expected type 'function', got '%s'", tc.Type)
	}
	if tc.Function.Name != "write_file" {
		t.Errorf("expected function name 'write_file', got '%s'", tc.Function.Name)
	}
	if tc.Function.Arguments != "{}" {
		t.Errorf("expected arguments '{}', got '%s'", tc.Function.Arguments)
	}
}

func TestConvertToHistoryMessages_ReasoningContent(t *testing.T) {
	input := []api.Message{
		{
			Role:             "assistant",
			Content:          "answer",
			ReasoningContent: "thinking step",
		},
	}

	result := convertToHistoryMessages(input)
	if result[0].ReasoningContent != "thinking step" {
		t.Errorf("expected reasoning 'thinking step', got '%s'", result[0].ReasoningContent)
	}
}

func TestConvertToHistoryMessages_ToolCallId(t *testing.T) {
	input := []api.Message{
		{
			Role:       "tool",
			Content:    "result",
			ToolCallID: "call_xyz",
		},
	}

	result := convertToHistoryMessages(input)
	if result[0].ToolCallID != "call_xyz" {
		t.Errorf("expected tool_call_id 'call_xyz', got '%s'", result[0].ToolCallID)
	}
}

func TestGenerateSessionID_Format(t *testing.T) {
	id := generateSessionID()
	if !strings.HasPrefix(id, "agent-") {
		t.Errorf("expected prefix 'agent-', got '%s'", id)
	}

	// Check that the suffix is numeric (UnixNano)
	suffix := strings.TrimPrefix(id, "agent-")
	for _, r := range suffix {
		if r < '0' || r > '9' {
			t.Errorf("expected numeric suffix, got non-digit '%c' in '%s'", r, suffix)
			break
		}
	}
}

func TestGenerateSessionID_Uniqueness(t *testing.T) {
	id1 := generateSessionID()
	// Use a retry loop instead of fixed sleep to avoid flakiness under CI load
	for i := 0; i < 50; i++ {
		id2 := generateSessionID()
		if id1 != id2 {
			return // success
		}
		time.Sleep(time.Millisecond)
	}
	t.Errorf("expected unique session IDs after retries")
}

func TestGenerateRevisionID_Format(t *testing.T) {
	id := generateRevisionID("session", "instructions")
	if len(id) != 16 {
		t.Errorf("expected length 16, got %d", len(id))
	}
	if !strings.HasPrefix(id, "agent-") {
		t.Errorf("expected prefix 'agent-', got '%s'", id)
	}
}

func TestGenerateRevisionID_DifferentInstructions(t *testing.T) {
	id1 := generateRevisionID("session", "instruction A")
	id2 := generateRevisionID("session", "instruction B")

	if id1 == id2 {
		t.Errorf("expected different revision IDs for different instructions")
	}
}

func TestGenerateRevisionID_DifferentSessions(t *testing.T) {
	id1 := generateRevisionID("session-A", "same instructions")
	id2 := generateRevisionID("session-B", "same instructions")

	if id1 == id2 {
		t.Errorf("expected different revision IDs for different sessions")
	}
}

func TestGenerateRevisionID_SameInputs(t *testing.T) {
	// With same inputs, the IDs incorporate nanosecond timestamps
	// so they should still be different (eventually). Use retry loop.
	id1 := generateRevisionID("session", "instructions")
	for i := 0; i < 50; i++ {
		id2 := generateRevisionID("session", "instructions")
		if id1 != id2 {
			return // success
		}
		time.Sleep(time.Millisecond)
	}
	t.Errorf("expected different revision IDs even for same inputs after retries")
}
