package tools

import (
	"strings"
	"testing"
	"time"
)

// waitWithTimeout runs fn in a goroutine and fails the test if it does not complete in time.
func waitWithTimeout(t *testing.T, timeout time.Duration, fn func() []TodoItem) []TodoItem {
	t.Helper()
	done := make(chan []TodoItem, 1)
	go func() {
		done <- fn()
	}()
	select {
	case res := <-done:
		return res
	case <-time.After(timeout):
		t.Fatalf("operation timed out after %v (possible deadlock)", timeout)
		return nil
	}
}

func TestTodoWrite_NoDeadlock(t *testing.T) {
	// Reset global state
	TodoWrite([]TodoItem{})

	todos := []TodoItem{
		{Content: "Set up project", Status: "pending", Priority: "high"},
		{Content: "Implement feature", Status: "pending", Priority: "medium"},
		{Content: "Write tests", Status: "pending", Priority: "low"},
	}

	res := waitWithTimeout(t, 2*time.Second, func() []TodoItem {
		TodoWrite(todos)
		return TodoRead()
	})

	if len(res) != 3 {
		t.Fatalf("expected 3 todos, got: %d", len(res))
	}

	// Verify we can call TodoRead without deadlock (this tests RLock usage)
	todos2 := waitWithTimeout(t, 1*time.Second, func() []TodoItem {
		return TodoRead()
	})

	if len(todos2) != 3 {
		t.Fatalf("expected 3 todos in second read, got: %d", len(todos2))
	}
}

func TestTodoRead_EmptyList(t *testing.T) {
	// Reset and read
	TodoWrite([]TodoItem{})

	todos := waitWithTimeout(t, 1*time.Second, func() []TodoItem {
		return TodoRead()
	})

	if len(todos) != 0 {
		t.Fatalf("expected 0 todos, got: %d", len(todos))
	}
}

func TestTodoWrite_OverwriteList(t *testing.T) {
	// Start with initial list
	TodoWrite([]TodoItem{
		{Content: "Initial task", Status: "pending"},
	})

	// Replace with new list
	TodoWrite([]TodoItem{
		{Content: "New task 1", Status: "pending"},
		{Content: "New task 2", Status: "in_progress"},
	})

	todos := TodoRead()

	if len(todos) != 2 {
		t.Fatalf("expected 2 todos after overwrite, got: %d", len(todos))
	}

	if todos[0].Content != "New task 1" {
		t.Fatalf("expected first task to be 'New task 1', got: %s", todos[0].Content)
	}
}

func TestTodoWrite_WithPriority(t *testing.T) {
	TodoWrite([]TodoItem{})

	todos := []TodoItem{
		{Content: "High priority task", Status: "pending", Priority: "high"},
		{Content: "Low priority task", Status: "pending", Priority: "low"},
	}
	TodoWrite(todos)

	result := TodoRead()

	if len(result) != 2 {
		t.Fatalf("expected 2 todos, got: %d", len(result))
	}

	// Check that priorities are preserved
	foundHigh := false
	for _, t := range result {
		if t.Priority == "high" && strings.Contains(t.Content, "High priority") {
			foundHigh = true
		}
	}
	if !foundHigh {
		t.Fatalf("expected to find high priority task")
	}
}

func TestGetTodoListCompact(t *testing.T) {
	TodoWrite([]TodoItem{
		{Content: "Task 1", Status: "pending"},
		{Content: "Task 2", Status: "in_progress"},
	})

	result := GetTodoListCompact()

	if len(result) != 2 {
		t.Fatalf("expected 2 todos, got: %d", len(result))
	}
}
