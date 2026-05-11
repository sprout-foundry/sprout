package tools

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	tm := NewTodoManager()

	todos := []TodoItem{
		{Content: "Set up project", Status: "pending", Priority: "high"},
		{Content: "Implement feature", Status: "pending", Priority: "medium"},
		{Content: "Write tests", Status: "pending", Priority: "low"},
	}

	res := waitWithTimeout(t, 2*time.Second, func() []TodoItem {
		tm.Write(todos)
		return tm.Read()
	})

	if len(res) != 3 {
		t.Fatalf("expected 3 todos, got: %d", len(res))
	}

	// Verify we can call Read without deadlock (this tests RLock usage)
	todos2 := waitWithTimeout(t, 1*time.Second, func() []TodoItem {
		return tm.Read()
	})

	if len(todos2) != 3 {
		t.Fatalf("expected 3 todos in second read, got: %d", len(todos2))
	}
}

func TestTodoRead_EmptyList(t *testing.T) {
	tm := NewTodoManager()

	todos := waitWithTimeout(t, 1*time.Second, func() []TodoItem {
		return tm.Read()
	})

	if len(todos) != 0 {
		t.Fatalf("expected 0 todos, got: %d", len(todos))
	}
}

func TestTodoWrite_OverwriteList(t *testing.T) {
	tm := NewTodoManager()

	// Start with initial list
	tm.Write([]TodoItem{
		{Content: "Initial task", Status: "pending"},
	})

	// Replace with new list
	tm.Write([]TodoItem{
		{Content: "New task 1", Status: "pending"},
		{Content: "New task 2", Status: "in_progress"},
	})

	todos := tm.Read()

	if len(todos) != 2 {
		t.Fatalf("expected 2 todos after overwrite, got: %d", len(todos))
	}

	if todos[0].Content != "New task 1" {
		t.Fatalf("expected first task to be 'New task 1', got: %s", todos[0].Content)
	}
}

func TestTodoWrite_WithPriority(t *testing.T) {
	tm := NewTodoManager()

	todos := []TodoItem{
		{Content: "High priority task", Status: "pending", Priority: "high"},
		{Content: "Low priority task", Status: "pending", Priority: "low"},
	}
	tm.Write(todos)

	result := tm.Read()

	if len(result) != 2 {
		t.Fatalf("expected 2 todos, got: %d", len(result))
	}

	// Check that priorities are preserved
	foundHigh := false
	for _, item := range result {
		if item.Priority == "high" && strings.Contains(item.Content, "High priority") {
			foundHigh = true
		}
	}
	if !foundHigh {
		t.Fatalf("expected to find high priority task")
	}
}

func TestTodoWrite_ClearList(t *testing.T) {
	tm := NewTodoManager()

	tm.Write([]TodoItem{
		{Content: "Task 1", Status: "pending"},
		{Content: "Task 2", Status: "in_progress"},
	})

	msg := tm.Write([]TodoItem{})
	require.NotNil(t, tm)
	assert.Equal(t, "Todo list cleared", msg)
	assert.Empty(t, tm.Read())
}

func TestTodoWrite_ReturnsCount(t *testing.T) {
	tm := NewTodoManager()

	msg := tm.Write([]TodoItem{
		{Content: "Task 1", Status: "pending"},
		{Content: "Task 2", Status: "in_progress"},
		{Content: "Task 3", Status: "completed"},
	})
	assert.Equal(t, "Todo list updated with 3 items", msg)
}

func TestTodoRead_ReturnsCopy(t *testing.T) {
	tm := NewTodoManager()

	tm.Write([]TodoItem{
		{Content: "Original", Status: "pending"},
	})

	// Modify the returned slice should not affect stored items
	result := tm.Read()
	result[0].Content = "Modified"

	original := tm.Read()
	assert.Equal(t, "Original", original[0].Content)
}

func TestTodoManager_ConcurrentReadWrite(t *testing.T) {
	tm := NewTodoManager()
	done := make(chan struct{})

	// Concurrent writers
	for i := 0; i < 5; i++ {
		go func(n int) {
			defer func() { done <- struct{}{} }()
			tm.Write([]TodoItem{
				{Content: "from goroutine", Status: "pending"},
			})
		}(i)
	}

	// Concurrent readers
	for i := 0; i < 5; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			tm.Read()
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("deadlock detected in concurrent read/write")
		}
	}
}
