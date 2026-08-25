package tools

import (
	"context"
	"testing"
)

// TestTodoWriteHandler_NonObjectElementsNoPanic ensures Execute does not panic
// when the LLM passes a todos array containing non-object elements (e.g. plain
// strings). Regression test for a production panic at todo_write_handler.go:68
// where an unchecked type assertion `todoRaw.(map[string]interface{})` crashed.
func TestTodoWriteHandler_NonObjectElementsNoPanic(t *testing.T) {
	h := &todoWriteHandler{}

	// A string element where an object is expected. Must not panic.
	args := map[string]any{
		"todos": []interface{}{
			"not a todo object",
		},
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Execute panicked on non-object todos element: %v", r)
		}
	}()

	result, err := h.Execute(context.Background(), ToolEnv{}, args)
	// No panic is the primary assertion. The handler should gracefully skip
	// the invalid element and return an empty result (or an error — either is
	// acceptable as long as it doesn't crash the agent process).
	_ = result
	_ = err
}

// TestTodoWriteHandler_NonArrayTodosNoPanic ensures Execute does not panic
// when the LLM passes a non-array value for the todos parameter.
func TestTodoWriteHandler_NonArrayTodosNoPanic(t *testing.T) {
	h := &todoWriteHandler{}

	args := map[string]any{
		"todos": "not an array",
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Execute panicked on non-array todos: %v", r)
		}
	}()

	_, err := h.Execute(context.Background(), ToolEnv{}, args)
	if err == nil {
		t.Error("expected error for non-array todos, got nil")
	}
}

// TestTodoWriteHandler_MixedValidAndInvalidElements ensures valid todos are
// still processed when invalid elements are mixed in.
func TestTodoWriteHandler_MixedValidAndInvalidElements(t *testing.T) {
	h := &todoWriteHandler{}

	args := map[string]any{
		"todos": []interface{}{
			map[string]interface{}{
				"content": "valid task",
				"status":  "pending",
			},
			"invalid string element",
			map[string]interface{}{
				"content": "another valid task",
				"status":  "in_progress",
			},
		},
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Execute panicked on mixed elements: %v", r)
		}
	}()

	result, err := h.Execute(context.Background(), ToolEnv{}, args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The result should mention 2 todos (the invalid one was skipped).
	if result.Output == "" {
		t.Error("expected non-empty output for mixed valid/invalid todos")
	}
}
