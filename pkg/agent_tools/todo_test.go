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
	msg := tm.Write([]TodoItem{
		{Content: "New task 1", Status: "pending"},
		{Content: "New task 2", Status: "in_progress"},
	})

	assert.Contains(t, msg, "Todo list updated with 2 items")
	assert.Contains(t, msg, "(1 in_progress, 1 pending)")
	assert.Contains(t, msg, "Remaining:")
	assert.Contains(t, msg, "[pending] New task 1")
	assert.Contains(t, msg, "[in_progress] New task 2")

	todos := tm.Read()
	assert.Equal(t, 2, len(todos))
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
	assert.Contains(t, msg, "Todo list updated with 3 items")
	assert.Contains(t, msg, "(1 completed, 1 in_progress, 1 pending)")
	assert.Contains(t, msg, "Remaining:")
	assert.Contains(t, msg, "[in_progress] Task 2")
	assert.Contains(t, msg, "[pending] Task 1")
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

func TestTodoWrite_AllCompleted(t *testing.T) {
	tm := NewTodoManager()

	msg := tm.Write([]TodoItem{
		{Content: "Task 1", Status: "completed"},
		{Content: "Task 2", Status: "completed"},
		{Content: "Task 3", Status: "completed"},
	})

	assert.Contains(t, msg, "Todo list updated with 3 items")
	assert.Contains(t, msg, "ALL COMPLETED")
	assert.Contains(t, msg, "Completed:")
	assert.Contains(t, msg, "[completed] Task 1")
	assert.Contains(t, msg, "[completed] Task 2")
	assert.Contains(t, msg, "[completed] Task 3")
	assert.NotContains(t, msg, "Remaining:")
}

func TestTodoWrite_MixedState(t *testing.T) {
	tm := NewTodoManager()

	msg := tm.Write([]TodoItem{
		{Content: "Fix bug in parser", Status: "in_progress"},
		{Content: "Write tests for parser", Status: "pending"},
		{Content: "Update documentation", Status: "pending"},
		{Content: "Fix bug in UI", Status: "completed"},
		{Content: "Fix bug in API", Status: "completed"},
	})

	assert.Contains(t, msg, "Todo list updated with 5 items")
	assert.Contains(t, msg, "(2 completed, 1 in_progress, 2 pending)")
	assert.Contains(t, msg, "Remaining:")
	assert.Contains(t, msg, "[in_progress] Fix bug in parser")
	assert.Contains(t, msg, "[pending] Write tests for parser")
	assert.Contains(t, msg, "[pending] Update documentation")
	// Completed items should NOT be in the Remaining list
	assert.NotContains(t, msg, "[completed] Fix bug in UI")
	assert.NotContains(t, msg, "[completed] Fix bug in API")
}

func TestTodoWrite_AllPending(t *testing.T) {
	tm := NewTodoManager()

	msg := tm.Write([]TodoItem{
		{Content: "Fix bug in parser", Status: "pending"},
		{Content: "Write tests for parser", Status: "pending"},
		{Content: "Update documentation", Status: "pending"},
	})

	assert.Contains(t, msg, "Todo list updated with 3 items")
	assert.Contains(t, msg, "(3 pending)")
	assert.Contains(t, msg, "Remaining:")
	assert.Contains(t, msg, "[pending] Fix bug in parser")
	assert.Contains(t, msg, "[pending] Write tests for parser")
	assert.Contains(t, msg, "[pending] Update documentation")
}

func TestTodoWrite_SingleItemCompleted(t *testing.T) {
	tm := NewTodoManager()

	msg := tm.Write([]TodoItem{
		{Content: "Single task", Status: "completed"},
	})

	assert.Contains(t, msg, "Todo list updated with 1 items")
	assert.Contains(t, msg, "ALL COMPLETED")
	assert.Contains(t, msg, "Completed:")
	assert.Contains(t, msg, "[completed] Single task")
}

func TestTodoWrite_LongContentTruncation(t *testing.T) {
	tm := NewTodoManager()

	// Create a task with very long content (> 80 chars)
	longContent := strings.Repeat("This is a very long task description that should be truncated ", 3)
	msg := tm.Write([]TodoItem{
		{Content: longContent, Status: "pending"},
	})

	assert.Contains(t, msg, "Remaining:")
	// Should be truncated with "..."
	assert.Contains(t, msg, "...")
	// Should not contain the full original string in the output (it's > 80 chars)
	assert.NotContains(t, msg, longContent)
}
