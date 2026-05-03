package agent

import (
	"context"
	"testing"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// TestHandleTodoWrite2 tests valid todo write
func TestHandleTodoWrite2(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	ctx := context.Background()
	args := map[string]interface{}{
		"todos": []interface{}{
			map[string]interface{}{
				"id":      "1",
				"content": "Task 1",
				"status":  "pending",
			},
		},
	}

	result, err := handleTodoWrite(ctx, a, args)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == "" {
		t.Error("expected non-empty result")
	}
}

// TestHandleTodoWrite_Error2 tests error handling
func TestHandleTodoWrite_Error2(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	ctx := context.Background()
	args := map[string]interface{}{
		// Missing 'todos' argument
	}

	result, err := handleTodoWrite(ctx, a, args)

	if err == nil {
		t.Fatal("expected error for missing todos argument")
	}

	if result != "" {
		t.Error("expected empty result on error")
	}
}

// TestHandleTodoRead2 tests reading todos
func TestHandleTodoRead2(t *testing.T) {
	tools.TodoWrite([]tools.TodoItem{})

	a := &Agent{}
	a.initSubManagers()

	ctx := context.Background()
	args := map[string]interface{}{}

	result, err := handleTodoRead(ctx, a, args)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result == "" {
		t.Error("expected non-empty result")
	}
}
