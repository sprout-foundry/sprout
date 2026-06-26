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

	t.Run("todo item not an object or string", func(t *testing.T) {
		_, err := handleTodoWrite(context.Background(), a, map[string]interface{}{
			"todos": []interface{}{42},
		})
		if err == nil {
			t.Error("expected error for non-object, non-string todo item")
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

// ---------------------------------------------------------------------------
// Coercion tests — malformed inputs from models that don't follow the schema
// ---------------------------------------------------------------------------

func TestCoerceTodoItem_BareString(t *testing.T) {
	item, err := coerceTodoItem("Do something")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if item.Content != "Do something" {
		t.Errorf("content = %q, want %q", item.Content, "Do something")
	}
	if item.Status != "pending" {
		t.Errorf("status = %q, want %q", item.Status, "pending")
	}
}

func TestCoerceTodoItem_AlternativeFieldNames(t *testing.T) {
	item, err := coerceTodoItem(map[string]interface{}{
		"text":  "Build the thing",
		"state": "done",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if item.Content != "Build the thing" {
		t.Errorf("content = %q, want %q", item.Content, "Build the thing")
	}
	if item.Status != "completed" {
		t.Errorf("status = %q, want %q (normalized from 'done')", item.Status, "completed")
	}
}

func TestCoerceTodoItem_StatusNormalization(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"done", "completed"},
		{"complete", "completed"},
		{"finished", "completed"},
		{"in-progress", "in_progress"},
		{"inProgress", "in_progress"},
		{"active", "in_progress"},
		{"started", "in_progress"},
		{"doing", "in_progress"},
		{"todo", "pending"},
		{"new", "pending"},
		{"not-started", "pending"},
		{"canceled", "cancelled"},
		{"skipped", "cancelled"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			item, err := coerceTodoItem(map[string]interface{}{
				"content": "Test task",
				"status":  tc.input,
			})
			if err != nil {
				t.Fatalf("unexpected error for status %q: %v", tc.input, err)
			}
			if item.Status != tc.expected {
				t.Errorf("status = %q, want %q (from %q)", item.Status, tc.expected, tc.input)
			}
		})
	}
}

func TestCoerceTodoItem_PriorityNormalization(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hi", "high"},
		{"high", "high"},
		{"med", "medium"},
		{"medium", "medium"},
		{"normal", "medium"},
		{"default", "medium"},
		{"lo", "low"},
		{"low", "low"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			item, err := coerceTodoItem(map[string]interface{}{
				"content":  "Test task",
				"status":   "pending",
				"priority": tc.input,
			})
			if err != nil {
				t.Fatalf("unexpected error for priority %q: %v", tc.input, err)
			}
			if item.Priority != tc.expected {
				t.Errorf("priority = %q, want %q (from %q)", item.Priority, tc.expected, tc.input)
			}
		})
	}
}

func TestCoerceTodosFromArgs_AlternativeTopLevelKey(t *testing.T) {
	args := map[string]interface{}{
		"tasks": []interface{}{
			map[string]interface{}{
				"content": "Task via 'tasks' key",
				"status":  "pending",
			},
		},
	}
	todos, err := coerceTodosFromArgs(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(todos) != 1 {
		t.Fatalf("expected 1 todo, got %d", len(todos))
	}
	if todos[0].Content != "Task via 'tasks' key" {
		t.Errorf("content = %q", todos[0].Content)
	}
}

func TestCoerceTodosFromArgs_JSONString(t *testing.T) {
	jsonStr := `[{"content":"Decoded task","status":"pending"}]`
	args := map[string]interface{}{
		"todos": jsonStr,
	}
	todos, err := coerceTodosFromArgs(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(todos) != 1 {
		t.Fatalf("expected 1 todo, got %d", len(todos))
	}
	if todos[0].Content != "Decoded task" {
		t.Errorf("content = %q", todos[0].Content)
	}
}

func TestCoerceTodosFromArgs_MixedItems(t *testing.T) {
	args := map[string]interface{}{
		"todos": []interface{}{
			map[string]interface{}{
				"content": "Map task",
				"status":  "in_progress",
			},
			"Bare string task",
		},
	}
	todos, err := coerceTodosFromArgs(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(todos) != 2 {
		t.Fatalf("expected 2 todos, got %d", len(todos))
	}
	if todos[0].Content != "Map task" {
		t.Errorf("first content = %q", todos[0].Content)
	}
	if todos[0].Status != "in_progress" {
		t.Errorf("first status = %q", todos[0].Status)
	}
	if todos[1].Content != "Bare string task" {
		t.Errorf("second content = %q", todos[1].Content)
	}
	if todos[1].Status != "pending" {
		t.Errorf("second status = %q", todos[1].Status)
	}
}

func TestCoerceTodosFromArgs_BackwardsCompatible(t *testing.T) {
	// Standard shape should still work exactly as before
	args := map[string]interface{}{
		"todos": []interface{}{
			map[string]interface{}{
				"content":    "Standard task",
				"status":     "pending",
				"priority":   "high",
				"id":         "todo_1",
				"activeForm": "Doing standard task",
			},
		},
	}
	todos, err := coerceTodosFromArgs(args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(todos) != 1 {
		t.Fatalf("expected 1 todo, got %d", len(todos))
	}
	todo := todos[0]
	if todo.Content != "Standard task" {
		t.Errorf("content = %q", todo.Content)
	}
	if todo.Status != "pending" {
		t.Errorf("status = %q", todo.Status)
	}
	if todo.Priority != "high" {
		t.Errorf("priority = %q", todo.Priority)
	}
	if todo.ID != "todo_1" {
		t.Errorf("id = %q", todo.ID)
	}
	if todo.ActiveForm != "Doing standard task" {
		t.Errorf("activeForm = %q", todo.ActiveForm)
	}
}

func TestHandleTodoWrite_CoercionViaHandler(t *testing.T) {
	a := &Agent{
		state: NewAgentStateManager(false),
	}

	t.Run("alternative key 'tasks' via handler", func(t *testing.T) {
		_, err := handleTodoWrite(context.Background(), a, map[string]interface{}{
			"tasks": []interface{}{
				map[string]interface{}{
					"content": "Via tasks key",
					"status":  "pending",
				},
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		read := a.GetTodoManager().Read()
		if len(read) != 1 {
			t.Fatalf("expected 1 todo, got %d", len(read))
		}
		if read[0].Content != "Via tasks key" {
			t.Errorf("content = %q", read[0].Content)
		}
	})

	t.Run("bare string item via handler", func(t *testing.T) {
		_, err := handleTodoWrite(context.Background(), a, map[string]interface{}{
			"todos": []interface{}{"Quick task"},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		read := a.GetTodoManager().Read()
		if len(read) != 1 {
			t.Fatalf("expected 1 todo, got %d", len(read))
		}
		if read[0].Content != "Quick task" {
			t.Errorf("content = %q", read[0].Content)
		}
		if read[0].Status != "pending" {
			t.Errorf("status = %q, want 'pending'", read[0].Status)
		}
	})

	t.Run("status normalization via handler", func(t *testing.T) {
		_, err := handleTodoWrite(context.Background(), a, map[string]interface{}{
			"todos": []interface{}{
				map[string]interface{}{
					"content": "Done task",
					"status":  "done",
				},
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		read := a.GetTodoManager().Read()
		if len(read) != 1 {
			t.Fatalf("expected 1 todo, got %d", len(read))
		}
		if read[0].Status != "completed" {
			t.Errorf("status = %q, want 'completed' (normalized from 'done')", read[0].Status)
		}
	})

	// Clean up
	tools.TodoWrite([]tools.TodoItem{})
}
