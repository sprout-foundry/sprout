package tools

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// TodoItem represents a single todo item
type TodoItem struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	Status      string    `json:"status"`             // pending, in_progress, completed, cancelled
	Priority    string    `json:"priority,omitempty"` // high, medium, low
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// TodoManager manages the todo list for the current session
type TodoManager struct {
	items []TodoItem
	mutex sync.RWMutex
}

var globalTodoManager = &TodoManager{
	items: make([]TodoItem, 0),
}

// AddTodo adds a new todo item
func AddTodo(title, description, priority string) string {
	globalTodoManager.mutex.Lock()
	defer globalTodoManager.mutex.Unlock()

	if priority == "" {
		priority = "medium"
	}

	item := TodoItem{
		ID:          fmt.Sprintf("todo_%d", len(globalTodoManager.items)+1),
		Title:       title,
		Description: description,
		Status:      "pending",
		Priority:    priority,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	globalTodoManager.items = append(globalTodoManager.items, item)
	return formatTodoSuccess(title, item.ID)
}

// AddBulkTodos adds multiple todo items at once
func AddBulkTodos(todos []struct {
	Title       string
	Description string
	Priority    string
}) string {
	globalTodoManager.mutex.Lock()

	// Store base ID for consistent ID tracking in response
	baseID := len(globalTodoManager.items) + 1

	for _, todo := range todos {
		priority := todo.Priority
		if priority == "" {
			priority = "medium"
		}

		item := TodoItem{
			ID:          fmt.Sprintf("todo_%d", len(globalTodoManager.items)+1),
			Title:       todo.Title,
			Description: todo.Description,
			Status:      "pending",
			Priority:    priority,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}

		globalTodoManager.items = append(globalTodoManager.items, item)
	}
	// Unlock before generating markdown to avoid deadlock (GetTodoListMarkdown uses RLock)
	globalTodoManager.mutex.Unlock()

	if len(todos) == 0 {
		return "No todos added"
	}

	if len(todos) == 1 {
		return formatTodoSuccess(todos[0].Title, fmt.Sprintf("todo_%d", baseID))
	}

	// Build summary for multiple todos
	var itemsFormatted []string
	maxToShow := MaxNumberTodosToShowFull
	if len(todos) > maxToShow {
		maxToShow = TidyMaxTodos
	}

	for i := 0; i < maxToShow && i < len(todos); i++ {
		itemsFormatted = append(itemsFormatted, formatTodoResponseForID(todos[i].Title, fmt.Sprintf("todo_%d", baseID+i)))
	}

	moreCount := 0
	if len(todos) > maxToShow {
		moreCount = len(todos) - maxToShow
	}

	return formatBulkTodoSuccess(len(todos), itemsFormatted, moreCount)
}

// UpdateTodoStatus updates the status of a todo item
func UpdateTodoStatus(id, status string) string {
	// Validate status before taking the lock
	if !IsValidStatus(status) {
		return FormatTodoStatusError(status)
	}

	globalTodoManager.mutex.Lock()

	for i, item := range globalTodoManager.items {
		if item.ID == id {
			globalTodoManager.items[i].Status = status
			globalTodoManager.items[i].UpdatedAt = time.Now()

			// Count remaining todos to optionally add context
			remainingCount := 0
			for _, todo := range globalTodoManager.items {
				if todo.Status == "pending" || todo.Status == "in_progress" {
					remainingCount++
				}
			}

			// Unlock before returning
			globalTodoManager.mutex.Unlock()
			return formatStatusUpdate(status, item.Title, item.ID, remainingCount)
		}
	}

	// Unlock before returning not found
	globalTodoManager.mutex.Unlock()

	return "Todo not found"
}

// FindTodoIDByTitle performs a case-insensitive exact title match and returns the ID if found.
func FindTodoIDByTitle(title string) (string, bool) {
	globalTodoManager.mutex.RLock()
	defer globalTodoManager.mutex.RUnlock()

	tnorm := strings.TrimSpace(strings.ToLower(title))
	if tnorm == "" {
		return "", false
	}
	for _, item := range globalTodoManager.items {
		if strings.ToLower(strings.TrimSpace(item.Title)) == tnorm {
			return item.ID, true
		}
	}
	return "", false
}

// UpdateTodoStatusBulk updates multiple todos at once to reduce tool calls
func UpdateTodoStatusBulk(updates []struct {
	ID     string
	Status string
}) string {
	globalTodoManager.mutex.Lock()
	defer globalTodoManager.mutex.Unlock()

	var results []string
	updateCount := 0

	for _, update := range updates {
		for i, item := range globalTodoManager.items {
			if item.ID == update.ID {
				if item.Status != update.Status {
					globalTodoManager.items[i].Status = update.Status
					globalTodoManager.items[i].UpdatedAt = time.Now()
					updateCount++

					results = append(results, formatStatusWithID(update.Status, item.Title, item.ID))
				}
				break
			}
		}
	}

	return formatBulkStatusSummary(updateCount, results)
}

// ListTodos returns a markdown-formatted list of all todos for UI display
func ListTodos() string {
	return GetTodoListMarkdown()
}

// ListAllTodos returns verbose format when full context is needed
func ListAllTodos() string {
	globalTodoManager.mutex.RLock()
	defer globalTodoManager.mutex.RUnlock()

	if len(globalTodoManager.items) == 0 {
		return "No todos"
	}

	var result strings.Builder

	statusGroups := map[string][]TodoItem{
		"in_progress": {},
		"pending":     {},
		"completed":   {},
		"cancelled":   {},
	}

	for _, item := range globalTodoManager.items {
		statusGroups[item.Status] = append(statusGroups[item.Status], item)
	}

	// Full verbose format for when complete context is needed
	for _, status := range []string{"in_progress", "pending", "completed", "cancelled"} {
		items := statusGroups[status]
		if len(items) == 0 {
			continue
		}

		result.WriteString(fmt.Sprintf("%s %s:\n", getStatusEmoji(status), status))
		for _, item := range items {
			priority := ""
			if item.Priority != "" {
				priority = fmt.Sprintf("[%s] ", strings.ToUpper(item.Priority))
			}
			result.WriteString(fmt.Sprintf("  %s%s (%s)", priority, item.Title, item.ID))
			if item.Description != "" {
				result.WriteString(fmt.Sprintf(": %s", item.Description))
			}
			result.WriteString("\n")
		}
		result.WriteString("\n")
	}

	return result.String()
}

// GetTaskSummary generates a markdown summary of completed work
func GetTaskSummary() string {
	globalTodoManager.mutex.RLock()
	defer globalTodoManager.mutex.RUnlock()

	if len(globalTodoManager.items) == 0 {
		return "No tasks tracked in this session."
	}

	var result strings.Builder
	result.WriteString("## 📋 Task Summary\n\n")

	completed := 0
	inProgress := 0
	pending := 0
	cancelled := 0

	var completedTasks []TodoItem
	var inProgressTasks []TodoItem

	for _, item := range globalTodoManager.items {
		switch item.Status {
		case "completed":
			completed++
			completedTasks = append(completedTasks, item)
		case "in_progress":
			inProgress++
			inProgressTasks = append(inProgressTasks, item)
		case "pending":
			pending++
		case "cancelled":
			cancelled++
		}
	}

	// Progress overview
	total := len(globalTodoManager.items)
	result.WriteString(fmt.Sprintf("**Progress:** %d/%d tasks completed", completed, total))
	if inProgress > 0 {
		result.WriteString(fmt.Sprintf(" (%d in progress)", inProgress))
	}
	result.WriteString("\n\n")

	// Show completed tasks
	if len(completedTasks) > 0 {
		result.WriteString("### ✅ Completed\n")
		for _, item := range completedTasks {
			result.WriteString(fmt.Sprintf("- %s", item.Title))
			if item.Description != "" {
				result.WriteString(fmt.Sprintf(": %s", item.Description))
			}
			result.WriteString("\n")
		}
		result.WriteString("\n")
	}

	// Show in progress tasks
	if len(inProgressTasks) > 0 {
		result.WriteString("### 🔄 In Progress\n")
		for _, item := range inProgressTasks {
			result.WriteString(fmt.Sprintf("- %s", item.Title))
			if item.Description != "" {
				result.WriteString(fmt.Sprintf(": %s", item.Description))
			}
			result.WriteString("\n")
		}
		result.WriteString("\n")
	}

	if pending > 0 {
		result.WriteString(fmt.Sprintf("### ⏳ %d tasks remaining\n\n", pending))
	}

	return result.String()
}

// ClearTodos clears all todos (for new sessions)
func ClearTodos() string {
	globalTodoManager.mutex.Lock()
	defer globalTodoManager.mutex.Unlock()

	count := len(globalTodoManager.items)
	globalTodoManager.items = make([]TodoItem, 0)
	return fmt.Sprintf("🗑️ Cleared %d todos", count)
}

// ArchiveCompleted removes completed todos from active memory to reduce context bloat
func ArchiveCompleted() string {
	globalTodoManager.mutex.Lock()
	defer globalTodoManager.mutex.Unlock()

	var activeItems []TodoItem
	archivedCount := 0

	for _, item := range globalTodoManager.items {
		if item.Status == "completed" || item.Status == "cancelled" {
			archivedCount++
		} else {
			activeItems = append(activeItems, item)
		}
	}

	globalTodoManager.items = activeItems

	if archivedCount == 0 {
		return "No todos to archive"
	}

	return fmt.Sprintf("Archived %d completed/cancelled todos", archivedCount)
}

// GetActiveTodosCompact returns minimal format focused on current work
func GetActiveTodosCompact() string {
	globalTodoManager.mutex.RLock()
	defer globalTodoManager.mutex.RUnlock()

	var active []string
	var inProgress *TodoItem

	for _, item := range globalTodoManager.items {
		if item.Status == "in_progress" {
			inProgress = &item
		} else if item.Status == "pending" {
			priority := getCompactPrioritySymbol(item.Priority)
			active = append(active, fmt.Sprintf("%s%s", priority, item.Title))
		}
	}

	if inProgress == nil && len(active) == 0 {
		return "All done"
	}

	var result strings.Builder
	if inProgress != nil {
		result.WriteString(fmt.Sprintf("► %s", inProgress.Title))
		if len(active) > 0 {
			result.WriteString(" | ")
		}
	}

	if len(active) > 0 {
		if len(active) <= 3 {
			result.WriteString(strings.Join(active, ", "))
		} else {
			result.WriteString(fmt.Sprintf("%s, %s, +%d more", active[0], active[1], len(active)-2))
		}
	}

	return result.String()
}

func getStatusEmoji(status string) string {
	switch status {
	case "pending":
		return "⏳"
	case "in_progress":
		return "🔄"
	case "completed":
		return "✅"
	case "cancelled":
		return "❌"
	default:
		return "📝"
	}
}

// getCompactStatusSymbol returns single-character status symbols for token efficiency
func getCompactStatusSymbol(status string) string {
	switch status {
	case "pending":
		return "○"
	case "in_progress":
		return "►"
	case "completed":
		return "✓"
	case "cancelled":
		return "✗"
	default:
		return "·"
	}
}

// getCompactPrioritySymbol returns compact priority symbols
func getCompactPrioritySymbol(priority string) string {
	switch priority {
	case "high":
		return "!"
	case "medium":
		return ""
	case "low":
		return "·"
	default:
		return ""
	}
}

// GetNextTodo returns the next logical todo based on current state
func GetNextTodo() string {
	globalTodoManager.mutex.RLock()
	defer globalTodoManager.mutex.RUnlock()

	// Find next in-progress or pending todo by priority
	var nextTodo *TodoItem
	for _, item := range globalTodoManager.items {
		if item.Status == "in_progress" {
			return fmt.Sprintf("🔄 Continue: %s (%s)", item.Title, item.ID)
		}
		if item.Status == "pending" {
			if nextTodo == nil || (item.Priority == "high" && nextTodo.Priority != "high") {
				nextTodo = &item
			}
		}
	}

	if nextTodo != nil {
		return fmt.Sprintf("⏳ Next: %s (%s)", nextTodo.Title, nextTodo.ID)
	}

	return "🎉 All todos completed!"
}

// GetCompletedTasks returns a list of completed task descriptions for session continuity
func GetCompletedTasks() []string {
	globalTodoManager.mutex.RLock()
	defer globalTodoManager.mutex.RUnlock()

	var completed []string
	for _, item := range globalTodoManager.items {
		if item.Status == "completed" {
			if item.Description != "" {
				completed = append(completed, fmt.Sprintf("%s: %s", item.Title, item.Description))
			} else {
				completed = append(completed, item.Title)
			}
		}
	}

	return completed
}

// GetTodoListMarkdown returns a markdown-formatted todo list for UI display
func GetTodoListMarkdown() string {
	globalTodoManager.mutex.RLock()
	defer globalTodoManager.mutex.RUnlock()

	if len(globalTodoManager.items) == 0 {
		return "No todos"
	}

	var result strings.Builder

	// Show todos in creation order for better context
	for _, item := range globalTodoManager.items {
		var checkbox string
		var statusIndicator string

		switch item.Status {
		case "completed":
			checkbox = "[x]"
			statusIndicator = " ✅"
		case "in_progress":
			checkbox = "[...]"
			statusIndicator += " 🔄"
		case "cancelled":
			checkbox = "[x]"
			statusIndicator = " ❌"
		default: // pending
			checkbox = "[ ]"
			statusIndicator = ""
		}

		// Use ID for reference since models need the exact ID for updates
		result.WriteString(fmt.Sprintf("- %s %s (%s)", checkbox, item.Title, item.ID))

		// Add priority indicator
		if item.Priority == "high" {
			result.WriteString(" ⚡")
		}

		// Add description if present
		if item.Description != "" {
			result.WriteString(fmt.Sprintf(" - %s", item.Description))
		}

		// Add status indicator at the end
		result.WriteString(statusIndicator)
		result.WriteString("\n")
	}

	return result.String()
}

// FindTodoID finds a todo ID by title or returns not found
func FindTodoID(title string) string {
	id, found := FindTodoIDByTitle(title)
	if !found {
		return fmt.Sprintf("Todo not found: %s", title)
	}
	return id
}

// RemoveTodo removes a todo by ID or title
func RemoveTodo(identifier string) string {
	globalTodoManager.mutex.Lock()
	defer globalTodoManager.mutex.Unlock()

	// First try exact ID match
	for i, item := range globalTodoManager.items {
		if item.ID == identifier {
			title := item.Title
			globalTodoManager.items = append(globalTodoManager.items[:i], globalTodoManager.items[i+1:]...)
			return fmt.Sprintf("🗑️ Removed todo: %s", title)
		}
	}

	// Try title match (case-insensitive)
	titleNorm := strings.ToLower(strings.TrimSpace(identifier))
	for i, item := range globalTodoManager.items {
		if strings.ToLower(strings.TrimSpace(item.Title)) == titleNorm {
			title := item.Title
			globalTodoManager.items = append(globalTodoManager.items[:i], globalTodoManager.items[i+1:]...)
			return fmt.Sprintf("🗑️ Removed todo: %s (by title)", title)
		}
	}

	return fmt.Sprintf("Todo not found: %s", identifier)
}
