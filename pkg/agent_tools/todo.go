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

var globalTodoManager = &TodoManager{
	items: make([]TodoItem, 0),
}

// TodoWrite creates and manages a structured task list for the current session
func TodoWrite(todos []TodoItem) string {
	globalTodoManager.mutex.Lock()
	defer globalTodoManager.mutex.Unlock()

	// Replace all items with the new list
	globalTodoManager.items = todos

	if len(todos) == 0 {
		return "Todo list cleared"
	}
	return fmt.Sprintf("Todo list updated with %d items", len(todos))
}

// TodoRead returns the current todo list
func TodoRead() []TodoItem {
	globalTodoManager.mutex.RLock()
	defer globalTodoManager.mutex.RUnlock()

	// Return a copy
	result := make([]TodoItem, len(globalTodoManager.items))
	copy(result, globalTodoManager.items)
	return result
}

// GetTodoListCompact returns a compact representation of the todo list
func GetTodoListCompact() []TodoItem {
	return TodoRead()
}
