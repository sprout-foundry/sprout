package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for the package-level TodoWrite and TodoRead convenience functions
// that delegate to the default TodoManager singleton.
//
// NOTE: These tests save and restore the package-level defaultManager
// using t.Cleanup to avoid cross-test contamination and race conditions.

func initDefaultManagerForTest(t *testing.T) {
	t.Helper()
	// Replace the default-scope manager in the registry with a fresh one
	// so each test starts clean. Restored on cleanup to avoid cross-test
	// contamination.
	saved := ManagerForChat("")
	todoRegistryLock.Lock()
	todoRegistry[""] = NewTodoManager()
	todoRegistryLock.Unlock()
	t.Cleanup(func() {
		todoRegistryLock.Lock()
		todoRegistry[""] = saved
		todoRegistryLock.Unlock()
	})
}

func TestTodoDefaultRead_InitiallyEmpty(t *testing.T) {
	initDefaultManagerForTest(t)

	todos := TodoRead()
	assert.Empty(t, todos, "TodoRead should return empty list when nothing has been written")
}

func TestTodoDefaultWriteAndRead_ItemsPersist(t *testing.T) {
	initDefaultManagerForTest(t)

	todos := []TodoItem{
		{ID: "1", Content: "Set up project", Status: "pending", Priority: "high"},
		{ID: "2", Content: "Implement feature", Status: "in_progress", Priority: "medium"},
		{ID: "3", Content: "Write tests", Status: "completed", Priority: "low"},
	}

	msg := TodoWrite(todos)
	assert.Contains(t, msg, "Todo list updated with 3 items")

	readBack := TodoRead()
	assert.Len(t, readBack, 3)
	assert.Equal(t, "Set up project", readBack[0].Content)
	assert.Equal(t, "pending", readBack[0].Status)
	assert.Equal(t, "Implement feature", readBack[1].Content)
	assert.Equal(t, "in_progress", readBack[1].Status)
	assert.Equal(t, "Write tests", readBack[2].Content)
	assert.Equal(t, "completed", readBack[2].Status)
}

func TestTodoDefaultWrite_ClearList(t *testing.T) {
	initDefaultManagerForTest(t)

	// First write some items
	TodoWrite([]TodoItem{
		{Content: "Task 1", Status: "pending"},
		{Content: "Task 2", Status: "in_progress"},
	})

	// Then clear
	msg := TodoWrite([]TodoItem{})
	assert.Equal(t, "Todo list cleared", msg)

	// Verify it's empty
	assert.Empty(t, TodoRead())
}

func TestTodoDefaultWrite_AllCompleted(t *testing.T) {
	initDefaultManagerForTest(t)

	msg := TodoWrite([]TodoItem{
		{Content: "Task 1", Status: "completed"},
		{Content: "Task 2", Status: "completed"},
	})

	assert.Contains(t, msg, "Todo list updated with 2 items")
	assert.Contains(t, msg, "ALL COMPLETED")
	assert.Contains(t, msg, "Completed:")
	assert.NotContains(t, msg, "Remaining:")

	readBack := TodoRead()
	assert.Len(t, readBack, 2)
}

func TestTodoDefaultWrite_MixedStatuses(t *testing.T) {
	initDefaultManagerForTest(t)

	msg := TodoWrite([]TodoItem{
		{Content: "Done task", Status: "completed"},
		{Content: "Working on it", Status: "in_progress"},
		{Content: "Not started", Status: "pending"},
	})

	assert.Contains(t, msg, "Todo list updated with 3 items")
	assert.Contains(t, msg, "1 completed")
	assert.Contains(t, msg, "1 in_progress")
	assert.Contains(t, msg, "1 pending")
	assert.Contains(t, msg, "Remaining:")

	// The Remaining section should only show in_progress and pending, not completed
	assert.Contains(t, msg, "[in_progress] Working on it")
	assert.Contains(t, msg, "[pending] Not started")
	assert.NotContains(t, msg, "[completed] Done task")
}

func TestTodoDefaultWrite_AllPending(t *testing.T) {
	initDefaultManagerForTest(t)

	msg := TodoWrite([]TodoItem{
		{Content: "Task A", Status: "pending"},
		{Content: "Task B", Status: "pending"},
	})

	assert.Contains(t, msg, "Todo list updated with 2 items")
	assert.Contains(t, msg, "(2 pending)")
	assert.Contains(t, msg, "Remaining:")
	assert.Contains(t, msg, "[pending] Task A")
	assert.Contains(t, msg, "[pending] Task B")
}

func TestTodoDefaultWrite_OverwritesPrevious(t *testing.T) {
	initDefaultManagerForTest(t)

	TodoWrite([]TodoItem{
		{Content: "Old task", Status: "pending"},
	})

	TodoWrite([]TodoItem{
		{Content: "New task", Status: "completed"},
	})

	readBack := TodoRead()
	assert.Len(t, readBack, 1)
	assert.Equal(t, "New task", readBack[0].Content)
}

func TestTodoDefaultRead_ReturnsCopy(t *testing.T) {
	initDefaultManagerForTest(t)

	TodoWrite([]TodoItem{
		{Content: "Original", Status: "pending"},
	})

	// Modify returned slice
	result := TodoRead()
	result[0].Content = "Modified"

	// Original should be unchanged
	original := TodoRead()
	assert.Equal(t, "Original", original[0].Content)
}
