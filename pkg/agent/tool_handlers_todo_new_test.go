package agent

import (
	"context"
	"strings"
	"testing"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

func TestHandleTodoWriteMissingArgsV2(t *testing.T) {
	a := newTestAgent(t)

	// Missing todos key
	_, err := handleTodoWrite(context.Background(), a, map[string]interface{}{})
	if err == nil {
		t.Error("expected error when todos argument is missing")
	}
	if err != nil && !strings.Contains(err.Error(), "missing todos argument") {
		t.Errorf("expected 'missing todos argument' error, got: %v", err)
	}
}

func TestHandleTodoWriteNotArrayV2(t *testing.T) {
	a := newTestAgent(t)

	// todos is not an array
	_, err := handleTodoWrite(context.Background(), a, map[string]interface{}{
		"todos": "not an array",
	})
	if err == nil {
		t.Error("expected error when todos is not an array")
	}
	if err != nil && !strings.Contains(err.Error(), "todos must be an array") {
		t.Errorf("expected 'todos must be an array' error, got: %v", err)
	}
}

func TestHandleTodoWriteNotObjectV2(t *testing.T) {
	a := newTestAgent(t)

	// Bare string items are now accepted (coerced to content + pending status)
	result, err := handleTodoWrite(context.Background(), a, map[string]interface{}{
		"todos": []interface{}{"not an object"},
	})
	if err != nil {
		t.Fatalf("bare string items should be accepted, got error: %v", err)
	}
	if !strings.Contains(result, "1 items") {
		t.Errorf("expected result to mention 1 item, got: %s", result)
	}

	// Verify the bare string was coerced correctly
	todos := a.GetTodoManager().Read()
	if len(todos) != 1 {
		t.Fatalf("expected 1 todo, got %d", len(todos))
	}
	if todos[0].Content != "not an object" {
		t.Errorf("content = %q; want %q", todos[0].Content, "not an object")
	}
	if todos[0].Status != "pending" {
		t.Errorf("status = %q; want %q", todos[0].Status, "pending")
	}
}

func TestHandleTodoWriteInvalidTypeV2(t *testing.T) {
	a := newTestAgent(t)

	// Numeric items should still fail (not object or string)
	_, err := handleTodoWrite(context.Background(), a, map[string]interface{}{
		"todos": []interface{}{42},
	})
	if err == nil {
		t.Error("expected error when todo element is neither object nor string")
	}
	if err != nil && !strings.Contains(err.Error(), "each todo must be an object or string") {
		t.Errorf("expected 'each todo must be an object or string' error, got: %v", err)
	}
}

func TestHandleTodoWriteMissingContentV2(t *testing.T) {
	a := newTestAgent(t)

	// Todo with no content
	_, err := handleTodoWrite(context.Background(), a, map[string]interface{}{
		"todos": []interface{}{
			map[string]interface{}{
				"status": "pending",
			},
		},
	})
	if err == nil {
		t.Error("expected error when todo content is missing")
	}
	if err != nil && !strings.Contains(err.Error(), "each todo requires content") {
		t.Errorf("expected 'each todo requires content' error, got: %v", err)
	}
}

func TestHandleTodoWriteMissingStatusV2(t *testing.T) {
	a := newTestAgent(t)

	// Todo with no status
	_, err := handleTodoWrite(context.Background(), a, map[string]interface{}{
		"todos": []interface{}{
			map[string]interface{}{
				"content": "Do something",
			},
		},
	})
	if err == nil {
		t.Error("expected error when todo status is missing")
	}
	if err != nil && !strings.Contains(err.Error(), "each todo requires status") {
		t.Errorf("expected 'each todo requires status' error, got: %v", err)
	}
}

func TestHandleTodoWriteValidV2(t *testing.T) {
	a := newTestAgent(t)
	defer tools.TodoWrite(nil) // cleanup global state

	result, err := handleTodoWrite(context.Background(), a, map[string]interface{}{
		"todos": []interface{}{
			map[string]interface{}{
				"content": "Build feature",
				"status":  "pending",
				"id":      "task-1",
			},
			map[string]interface{}{
				"content":  "Write tests",
				"status":   "in_progress",
				"priority": "high",
				"id":       "task-2",
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "2 items") {
		t.Errorf("expected result to mention 2 items, got: %s", result)
	}

	// Verify the agent's todo manager actually has the items
	todos := a.GetTodoManager().Read()
	if len(todos) != 2 {
		t.Fatalf("expected 2 todos in manager, got %d", len(todos))
	}
	if todos[0].Content != "Build feature" {
		t.Errorf("first todo content = %q; want %q", todos[0].Content, "Build feature")
	}
	if todos[0].Status != "pending" {
		t.Errorf("first todo status = %q; want %q", todos[0].Status, "pending")
	}
	if todos[0].ID != "task-1" {
		t.Errorf("first todo id = %q; want %q", todos[0].ID, "task-1")
	}
	if todos[1].Content != "Write tests" {
		t.Errorf("second todo content = %q; want %q", todos[1].Content, "Write tests")
	}
	if todos[1].Priority != "high" {
		t.Errorf("second todo priority = %q; want %q", todos[1].Priority, "high")
	}
}

func TestHandleTodoWriteEmptyListV2(t *testing.T) {
	a := newTestAgent(t)

	result, err := handleTodoWrite(context.Background(), a, map[string]interface{}{
		"todos": []interface{}{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "cleared") {
		t.Errorf("expected result to mention cleared, got: %s", result)
	}
}

func TestHandleTodoWriteOptionalFieldsV2(t *testing.T) {
	a := newTestAgent(t)

	// id and priority are optional — only content and status are required
	result, err := handleTodoWrite(context.Background(), a, map[string]interface{}{
		"todos": []interface{}{
			map[string]interface{}{
				"content": "Simple task",
				"status":  "completed",
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "1 items") {
		t.Errorf("expected result to mention 1 item, got: %s", result)
	}
}

func TestHandleTodoReadEmptyV2(t *testing.T) {
	a := newTestAgent(t)

	// Clear todos first
	tools.TodoWrite(nil)
	defer tools.TodoWrite(nil) // cleanup global state

	result, err := handleTodoRead(context.Background(), a, map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "No todos" {
		t.Errorf("handleTodoRead() = %q; want %q", result, "No todos")
	}
}

func TestHandleTodoReadWithItemsV2(t *testing.T) {
	a := newTestAgent(t)
	defer a.GetTodoManager().Write(nil) // cleanup

	// Set up some todos in the agent's manager
	a.GetTodoManager().Write([]tools.TodoItem{
		{Content: "Task A", Status: "pending"},
		{Content: "Task B", Status: "in_progress"},
		{Content: "Task C", Status: "completed"},
	})

	result, err := handleTodoRead(context.Background(), a, map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check each line
	if !strings.Contains(result, "[p] Task A") {
		t.Errorf("expected '[p] Task A' in result: %s", result)
	}
	if !strings.Contains(result, "[a] Task B") {
		// in_progress → active, so first letter is 'a'
		t.Errorf("expected '[a] Task B' in result (in_progress maps to active): %s", result)
	}
	if !strings.Contains(result, "[c] Task C") {
		t.Errorf("expected '[c] Task C' in result: %s", result)
	}
}

func TestHandleTodoReadStatusMappingV2(t *testing.T) {
	a := newTestAgent(t)
	defer a.GetTodoManager().Write(nil) // cleanup

	// in_progress should display as "active" in TodoRead
	a.GetTodoManager().Write([]tools.TodoItem{
		{Content: "In progress task", Status: "in_progress"},
	})

	result, err := handleTodoRead(context.Background(), a, map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// status "in_progress" → first char of "active" = 'a'
	if !strings.Contains(result, "[a] In progress task") {
		t.Errorf("expected '[a] In progress task' (in_progress → active): %s", result)
	}
}
