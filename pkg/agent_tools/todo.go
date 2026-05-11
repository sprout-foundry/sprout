package tools

import (
	"fmt"
	"sync"
)

// TodoItem represents a single todo item matching Claude Code's TodoWrite/TodoRead schema
type TodoItem struct {
	ID       string `json:"id"`
	Content  string `json:"content"`
	Status   string `json:"status"`   // pending, in_progress, completed
	Priority string `json:"priority"` // high, medium, low
}

// TodoManager manages the todo list for the current session
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
	return fmt.Sprintf("Todo list updated with %d items", len(todos))
}

// Read returns a copy of the current todo list.
func (tm *TodoManager) Read() []TodoItem {
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()

	result := make([]TodoItem, len(tm.items))
	copy(result, tm.items)
	return result
}
