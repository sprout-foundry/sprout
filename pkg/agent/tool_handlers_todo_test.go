package agent

import (
	"context"
	"strings"
	"testing"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

func containsSubstring(s, sub string) bool {
	return strings.Contains(s, sub)
}

func TestHandleTodoWrite(t *testing.T) {
	// Not parallel: uses global tools.TodoWrite singleton
	a := &Agent{
		state: NewAgentStateManager(false),
	}

	t.Run("missing todos argument", func(t *testing.T) {
		_, err := handleTodoWrite(context.Background(), a, map[string]interface{}{})
		if err == nil {
			t.Error("expected error for missing todos argument")
		}
		if err != nil && !containsSubstring(err.Error(), "missing todos argument") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("todos not an array", func(t *testing.T) {
		_, err := handleTodoWrite(context.Background(), a, map[string]interface{}{
			"todos": "not an array",
		})
		if err == nil {
			t.Error("expected error for non-array todos")
		}
		if err != nil && !containsSubstring(err.Error(), "todos must be an array") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("todo item not an object", func(t *testing.T) {
		_, err := handleTodoWrite(context.Background(), a, map[string]interface{}{
			"todos": []interface{}{"not an object"},
		})
		if err == nil {
			t.Error("expected error for non-object todo item")
		}
	})

	t.Run("todo missing content", func(t *testing.T) {
		_, err := handleTodoWrite(context.Background(), a, map[string]interface{}{
			"todos": []interface{}{
				map[string]interface{}{
					"status": "pending",
				},
			},
		})
		if err == nil {
			t.Error("expected error for missing content")
		}
	})

	t.Run("todo missing status", func(t *testing.T) {
		_, err := handleTodoWrite(context.Background(), a, map[string]interface{}{
			"todos": []interface{}{
				map[string]interface{}{
					"content": "Do something",
				},
			},
		})
		if err == nil {
			t.Error("expected error for missing status")
		}
	})

	t.Run("valid todos", func(t *testing.T) {
		result, err := handleTodoWrite(context.Background(), a, map[string]interface{}{
			"todos": []interface{}{
				map[string]interface{}{
					"content":  "Task 1",
					"status":   "pending",
					"priority": "high",
					"id":       "t1",
				},
				map[string]interface{}{
					"content": "Task 2",
					"status":  "in_progress",
				},
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == "" {
			t.Fatal("expected non-empty result")
		}

		// Verify the todos were actually written to the agent's manager
		read := a.GetTodoManager().Read()
		if len(read) != 2 {
			t.Errorf("expected 2 todos after write, got %d", len(read))
		}
	})

	t.Run("empty todos array", func(t *testing.T) {
		result, err := handleTodoWrite(context.Background(), a, map[string]interface{}{
			"todos": []interface{}{},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "Todo list cleared" {
			t.Errorf("got %q, want %q", result, "Todo list cleared")
		}
	})

	// Clean up global state
	tools.TodoWrite([]tools.TodoItem{})
}

func TestHandleTodoRead(t *testing.T) {
	// Not parallel: uses global tools.TodoWrite/TodoRead singleton
	a := &Agent{
		state: NewAgentStateManager(false),
	}

	t.Run("empty todo list", func(t *testing.T) {
		tools.TodoWrite([]tools.TodoItem{})
		result, err := handleTodoRead(context.Background(), a, map[string]interface{}{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "No todos" {
			t.Errorf("got %q, want %q", result, "No todos")
		}
	})

	t.Run("with todos", func(t *testing.T) {
		a := &Agent{
			state: NewAgentStateManager(false),
		}
		a.GetTodoManager().Write([]tools.TodoItem{
			{Content: "First task", Status: "pending"},
			{Content: "Second task", Status: "in_progress"},
			{Content: "Third task", Status: "completed"},
		})
		result, err := handleTodoRead(context.Background(), a, map[string]interface{}{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == "" {
			t.Fatal("expected non-empty result")
		}
		// Verify status abbreviation: first char of status
		if !containsSubstring(result, "[p] First task") {
			t.Errorf("expected pending status to appear as [p], got: %s", result)
		}
		// in_progress maps to "active" so first char is "a"
		if !containsSubstring(result, "[a] Second task") {
			t.Errorf("expected in_progress status to appear as [a] (active), got: %s", result)
		}
		if !containsSubstring(result, "[c] Third task") {
			t.Errorf("expected completed status to appear as [c], got: %s", result)
		}
	})

	// Clean up global state
	tools.TodoWrite([]tools.TodoItem{})
}

func TestHandleTodoWrite_InvalidStatus(t *testing.T) {
	// Not parallel: uses global tools.TodoWrite singleton
	a := &Agent{
		state: NewAgentStateManager(false),
	}

	_, err := handleTodoWrite(context.Background(), a, map[string]interface{}{
		"todos": []interface{}{
			map[string]interface{}{
				"content": "Test task",
				"status":  "invalid_status",
			},
		},
	})
	if err == nil {
		t.Fatal("expected error for invalid status, got nil")
	}
	if !strings.Contains(err.Error(), "invalid status") {
		t.Errorf("error should mention invalid status, got: %v", err)
	}

	// Clean up global state
	tools.TodoWrite([]tools.TodoItem{})
}

func TestHandleTodoWrite_InvalidPriority(t *testing.T) {
	// Not parallel: uses global tools.TodoWrite singleton
	a := &Agent{
		state: NewAgentStateManager(false),
	}

	_, err := handleTodoWrite(context.Background(), a, map[string]interface{}{
		"todos": []interface{}{
			map[string]interface{}{
				"content":  "Test task",
				"status":   "pending",
				"priority": "urgent",
			},
		},
	})
	if err == nil {
		t.Fatal("expected error for invalid priority, got nil")
	}
	if !strings.Contains(err.Error(), "invalid priority") {
		t.Errorf("error should mention invalid priority, got: %v", err)
	}

	// Clean up global state
	tools.TodoWrite([]tools.TodoItem{})
}
