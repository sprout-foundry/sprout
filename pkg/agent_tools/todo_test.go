package tools

import (
	"strings"
	"testing"
	"time"
)

// waitWithTimeout runs fn in a goroutine and fails the test if it does not complete in time.
func waitWithTimeout(t *testing.T, timeout time.Duration, fn func() string) string {
	t.Helper()
	done := make(chan string, 1)
	go func() {
		done <- fn()
	}()
	select {
	case res := <-done:
		return res
	case <-time.After(timeout):
		t.Fatalf("operation timed out after %v (possible deadlock)", timeout)
		return ""
	}
}

func TestAddBulkTodos_NoDeadlockAndMarkdown(t *testing.T) {
	// Reset global state
	ClearTodos()

	todos := []struct {
		Title       string
		Description string
		Priority    string
	}{
		{Title: "Set up project", Description: "Initialize repo and modules", Priority: "high"},
		{Title: "Implement feature", Description: "Add core logic", Priority: "medium"},
		{Title: "Write tests", Description: "Cover main flows", Priority: "low"},
	}

	res := waitWithTimeout(t, 2*time.Second, func() string {
		return AddBulkTodos(todos)
	})

	if !strings.Contains(res, "📝 Added 3 todo(s)") {
		t.Fatalf("expected added summary, got: %q", res)
	}
	if !strings.Contains(res, "**Todo List:**") {
		t.Fatalf("expected todo list markdown, got: %q", res)
	}
	if !strings.Contains(res, "- [ ] Set up project") {
		t.Fatalf("expected first todo in markdown list, got: %q", res)
	}
}

func TestUpdateTodoStatus_NoDeadlockAndMarkdown(t *testing.T) {
	// Reset and add two todos
	ClearTodos()
	_ = AddBulkTodos([]struct {
		Title       string
		Description string
		Priority    string
	}{
		{Title: "Task A", Description: "desc", Priority: ""},
		{Title: "Task B", Description: "desc", Priority: ""},
	})

	res := waitWithTimeout(t, 2*time.Second, func() string {
		return UpdateTodoStatus("todo_1", "completed")
	})

	if !strings.Contains(res, "**Todo List:**") {
		t.Fatalf("expected todo list markdown, got: %q", res)
	}
	// With two todos, completing one should show the completed header
	if !strings.Contains(res, "✅ Completed: Task A") && !strings.Contains(res, "🎉 All todos completed!") {
		t.Fatalf("expected completion header, got: %q", res)
	}

	// Ensure status reflected in markdown list
	if !strings.Contains(res, "- [x] Task A") {
		t.Fatalf("expected completed checkbox for Task A, got: %q", res)
	}
}

func TestUpdateTodoStatus_InvalidStatusDoesNotLock(t *testing.T) {
	// Reset and add one todo
	ClearTodos()
	_ = AddBulkTodos([]struct {
		Title       string
		Description string
		Priority    string
	}{
		{Title: "Task A", Description: "desc", Priority: ""},
	})

	// Call with invalid status and ensure it returns and does not hold the lock
	res := waitWithTimeout(t, 1*time.Second, func() string {
		return UpdateTodoStatus("todo_1", "invalid_status")
	})
	if !strings.Contains(res, "Invalid status:") {
		t.Fatalf("expected invalid status message, got: %q", res)
	}

	// Now try another operation that needs the same mutex; it should complete
	_ = waitWithTimeout(t, 1*time.Second, func() string {
		return AddTodo("Another", "", "")
	})
}

func TestUpdateTodoStatus_NotFoundDoesNotLock(t *testing.T) {
	ClearTodos()
	_ = AddBulkTodos([]struct {
		Title       string
		Description string
		Priority    string
	}{
		{Title: "Task X", Description: "desc", Priority: ""},
	})

	res := waitWithTimeout(t, 1*time.Second, func() string {
		return UpdateTodoStatus("todo_999", "completed")
	})
	if res != "Todo not found" {
		t.Fatalf("expected 'Todo not found', got: %q", res)
	}

	// Subsequent list should return promptly
	_ = waitWithTimeout(t, 1*time.Second, func() string { return ListTodos() })
}
