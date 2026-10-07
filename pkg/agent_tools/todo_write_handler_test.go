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

// TestTodoWriteHandler_Scope pins the seed-path extraction of the optional plan
// scope ID: Execute reads the "scope" field off each todo item
// and carries it onto the TodoItem it writes, so the live dispatch path
// (todo_write via the seed registry) preserves the plan link. A todo with no
// scope key yields an empty Scope (unlinked).
func TestTodoWriteHandler_Scope(t *testing.T) {
	h := &todoWriteHandler{}
	mgr := NewTodoManager()
	env := ToolEnv{TodoManager: mgr}

	args := map[string]any{
		"todos": []interface{}{
			map[string]interface{}{
				"content": "Implement login",
				"status":  "in_progress",
				"scope":   "s1",
			},
			map[string]interface{}{
				"content": "Build session UI",
				"status":  "pending",
			},
		},
	}

	_, err := h.Execute(context.Background(), env, args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	todos := mgr.Read()
	if len(todos) != 2 {
		t.Fatalf("expected 2 todos in the manager, got %d", len(todos))
	}
	if todos[0].Scope != "s1" {
		t.Errorf("todo[0].Scope = %q, want %q (the plan scope id)", todos[0].Scope, "s1")
	}
	if todos[1].Scope != "" {
		t.Errorf("todo[1].Scope = %q, want empty (no scope provided)", todos[1].Scope)
	}
}
