package tools

import (
	"fmt"
	"strings"
	"sync"
)

// TodoItem represents a single todo item matching Claude Code's TodoWrite/TodoRead schema.
//
// ActiveForm is the present-continuous phrasing surfaced in the activity
// indicator while Status == "in_progress" (e.g. "Implementing X" vs
// the imperative Content "Implement X"). Priority drives the colored
// indicator on the UI; it's accepted from the LLM but is purely
// presentational.
type TodoItem struct {
	ID         string `json:"id"`
	Content    string `json:"content"`
	Status     string `json:"status"`               // pending, in_progress, completed, cancelled
	Priority   string `json:"priority,omitempty"`   // high, medium, low
	ActiveForm string `json:"activeForm,omitempty"` // present-continuous phrasing
}

// TodoManager manages the todo list for a single conversation scope.
type TodoManager struct {
	items []TodoItem
	mutex sync.RWMutex
}

// NewTodoManager creates a new TodoManager instance.
func NewTodoManager() *TodoManager {
	return &TodoManager{
		items: make([]TodoItem, 0),
	}
}

// Write replaces all todo items with the new list and returns a status message.
func (tm *TodoManager) Write(todos []TodoItem) string {
	tm.mutex.Lock()
	defer tm.mutex.Unlock()

	tm.items = todos

	if len(todos) == 0 {
		return "Todo list cleared"
	}

	// Count items by status
	statusCounts := make(map[string]int)
	for _, todo := range todos {
		statusCounts[todo.Status]++
	}

	completed := statusCounts["completed"]
	inProgress := statusCounts["in_progress"]
	pending := statusCounts["pending"]

	// Build the first line with status summary
	var result strings.Builder
	if completed > 0 && inProgress == 0 && pending == 0 {
		// All completed
		result.WriteString(fmt.Sprintf("Todo list updated with %d items — ALL COMPLETED\n", len(todos)))
	} else {
		// Mixed or not all completed
		result.WriteString(fmt.Sprintf("Todo list updated with %d items", len(todos)))
		var parts []string
		if completed > 0 {
			parts = append(parts, fmt.Sprintf("%d completed", completed))
		}
		if inProgress > 0 {
			parts = append(parts, fmt.Sprintf("%d in_progress", inProgress))
		}
		if pending > 0 {
			parts = append(parts, fmt.Sprintf("%d pending", pending))
		}
		if len(parts) > 0 {
			result.WriteString(fmt.Sprintf(" (%s)", strings.Join(parts, ", ")))
		}
		result.WriteString("\n")
	}

	// Determine which items to show
	var itemsToShow []TodoItem
	var listHeader string

	if completed > 0 && inProgress == 0 && pending == 0 {
		// All completed - show all completed items
		itemsToShow = todos
		listHeader = "Completed:"
	} else if inProgress > 0 || pending > 0 {
		// Show in-progress and pending items (what remains)
		for _, todo := range todos {
			if todo.Status == "in_progress" || todo.Status == "pending" {
				itemsToShow = append(itemsToShow, todo)
			}
		}
		listHeader = "Remaining:"
	}

	// Show the items list if there are any
	if len(itemsToShow) > 0 {
		result.WriteString(fmt.Sprintf("\n%s\n", listHeader))
		for _, todo := range itemsToShow {
			content := todo.Content
			if todo.Status == "in_progress" && strings.TrimSpace(todo.ActiveForm) != "" {
				content = todo.ActiveForm
			}
			// Truncate to ~80 chars if needed
			if len(content) > 80 {
				content = content[:80] + "..."
			}
			result.WriteString(fmt.Sprintf("  - [%s] %s\n", todo.Status, content))
		}
	}

	return result.String()
}

// Read returns a copy of the current todo list.
func (tm *TodoManager) Read() []TodoItem {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()

	result := make([]TodoItem, len(tm.items))
	copy(result, tm.items)
	return result
}

// ─────────────────────────────────────────────────────────────────────────────
// Per-chat manager registry
// ─────────────────────────────────────────────────────────────────────────────

// todoRegistry holds one TodoManager per chat scope. The empty string key
// is the process-default scope used in CLI-only mode and by callers that
// don't have a chat_id (CLI commands, tests). Multiple concurrent chats
// each get their own list so they don't clobber each other.
var (
	todoRegistry     = make(map[string]*TodoManager)
	todoRegistryLock sync.RWMutex
)

// ManagerForChat returns the TodoManager for the given chat scope,
// lazily creating one if needed. A zero scope returns the process-default
// manager (used by CLI/non-chat tool invocations).
func ManagerForChat(chatID string) *TodoManager {
	key := strings.TrimSpace(chatID)
	todoRegistryLock.RLock()
	mgr, ok := todoRegistry[key]
	todoRegistryLock.RUnlock()
	if ok {
		return mgr
	}
	todoRegistryLock.Lock()
	defer todoRegistryLock.Unlock()
	if mgr, ok = todoRegistry[key]; ok {
		return mgr
	}
	mgr = NewTodoManager()
	todoRegistry[key] = mgr
	return mgr
}

// ResetTodoManagerForChat clears a chat's todo list (used by chat-end /
// session-reset flows). Safe to call with an unknown chat_id.
func ResetTodoManagerForChat(chatID string) {
	key := strings.TrimSpace(chatID)
	todoRegistryLock.Lock()
	defer todoRegistryLock.Unlock()
	delete(todoRegistry, key)
}

// TodoWrite is a convenience wrapper that writes todos to the default scope.
func TodoWrite(todos []TodoItem) string {
	return ManagerForChat("").Write(todos)
}

// TodoRead is a convenience wrapper that reads todos from the default scope.
func TodoRead() []TodoItem {
	return ManagerForChat("").Read()
}
