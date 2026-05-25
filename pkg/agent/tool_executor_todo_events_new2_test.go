package agent

import (
	"strings"
	"testing"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// TestTodoStatusSymbol2 tests status symbol mapping (unique name).
// Symbols are SP-057 glyphs; assert on the visible rune.
func TestTodoStatusSymbol2(t *testing.T) {
	tests := []struct {
		name     string
		status   string
		wantRune string
	}{
		{"pending", "pending", "·"},
		{"in_progress", "in_progress", "→"},
		{"completed", "completed", "✓"},
		{"cancelled", "cancelled", "⏹"},
		{"unknown", "unknown", "ⓘ"},
		{"empty", "", "ⓘ"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := todoStatusSymbol(tt.status)
			if !strings.Contains(result, tt.wantRune) {
				t.Errorf("todoStatusSymbol(%q) = %q; want contains %q", tt.status, result, tt.wantRune)
			}
		})
	}
}

// TestEmitTodoChecklistUpdate2 tests that emit doesn't panic with empty data
func TestEmitTodoChecklistUpdate2(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	te := &ToolExecutor{
		agent: a,
	}

	before := []tools.TodoItem{}
	after := []tools.TodoItem{}

	// Should not panic
	te.emitTodoChecklistUpdate(before, after)
}

// TestEmitTodoChecklistUpdate_WithItems2 tests status counting and change detection
func TestEmitTodoChecklistUpdate_WithItems2(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	te := &ToolExecutor{
		agent: a,
	}

	before := []tools.TodoItem{
		{ID: "1", Content: "Task A", Status: "pending"},
		{ID: "2", Content: "Task B", Status: "in_progress"},
	}

	after := []tools.TodoItem{
		{ID: "1", Content: "Task A", Status: "completed"},
		{ID: "2", Content: "Task B", Status: "completed"},
		{ID: "3", Content: "Task C", Status: "pending"},
	}

	// Should not panic; exercises status counting (0 pending, 0 in_progress, 2 completed)
	// and change detection (2 status changes + 1 new item)
	te.emitTodoChecklistUpdate(before, after)
}

// TestEmitTodoChecklistUpdate_NoChanges2 tests when before and after are identical
func TestEmitTodoChecklistUpdate_NoChanges2(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	te := &ToolExecutor{
		agent: a,
	}

	items := []tools.TodoItem{
		{ID: "1", Content: "Task A", Status: "pending"},
	}

	// Same items in before and after — no changes detected
	te.emitTodoChecklistUpdate(items, items)
}

// TestEmitTodoChecklistUpdate_NilAgent2 tests nil agent guard
func TestEmitTodoChecklistUpdate_NilAgent2(t *testing.T) {
	te := &ToolExecutor{
		agent: nil,
	}

	// Should not panic with nil agent
	te.emitTodoChecklistUpdate(nil, []tools.TodoItem{
		{ID: "1", Content: "Task", Status: "pending"},
	})
}
