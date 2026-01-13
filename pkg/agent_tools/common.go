package tools

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	// MaxNumberTodosToShowFull defines maximum todos to display fully in summaries
	MaxNumberTodosToShowFull = 3
	// TidyMaxTodos is the maximum to show when many todos exist
	TidyMaxTodos = 2
)

// ValidTodos contains all allowable todo status values
var ValidTodos = map[string]bool{
	"pending":     true,
	"in_progress": true,
	"completed":   true,
	"cancelled":   true,
}

// ValidTodoStatuses returns a slice of all valid status values for error messages
func ValidTodoStatuses() []string {
	return []string{"pending", "in_progress", "completed", "cancelled"}
}

// NormalizeTodoID converts various ID formats to the internal "todo_X" format.
// Accepted inputs:
//   - string: "todo_1" -> "todo_1", "1" -> "todo_1"
//   - float64: 1.0 -> "todo_1"
//   - int: 1 -> "todo_1"
//
// Returns empty string for unsupported types.
func NormalizeTodoID(id interface{}) string {
	switch v := id.(type) {
	case string:
		if v == "" {
			return ""
		}
		// Already in correct format
		if strings.HasPrefix(v, "todo_") {
			return v
		}
		// Numeric string, convert to todo_X
		if _, err := strconv.Atoi(v); err == nil {
			return fmt.Sprintf("todo_%s", v)
		}
		// Not a known format, return as-is (may be a title)
		return v
	case float64:
		return fmt.Sprintf("todo_%d", int(v))
	case int:
		return fmt.Sprintf("todo_%d", v)
	default:
		return ""
	}
}

// IsValidStatus checks if the given status string is valid.
func IsValidStatus(status string) bool {
	if status == "" {
		return false
	}
	return ValidTodos[status]
}

// FormatTodoStatusError returns a standardized error message for invalid status values.
func FormatTodoStatusError(status string) string {
	return fmt.Sprintf("invalid status '%s', must be one of: %s", status, strings.Join(ValidTodoStatuses(), ", "))
}

// formatTodoResponseForID formats a todo item title with its ID in square brackets.
func formatTodoResponseForID(title, id string) string {
	return fmt.Sprintf("%s [%s]", title, id)
}

// formatTodoSuccess formats a success message for adding a single todo.
func formatTodoSuccess(title, id string) string {
	return fmt.Sprintf("✅ Added todo: %s", formatTodoResponseForID(title, id))
}

// formatBulkTodoSuccess formats a success message for adding multiple todos.
func formatBulkTodoSuccess(count int, items []string, moreCount int) string {
	itemsStr := strings.Join(items, ", ")
	if moreCount > 0 {
		return fmt.Sprintf("📝 Added %d todos: %s, +%d more", count, itemsStr, moreCount)
	}
	return fmt.Sprintf("📝 Added %d todos: %s", count, itemsStr)
}

// formatStatusUpdate formats a status update message for a todo.
func formatStatusUpdate(status, title, id string, remaining int) string {
	switch status {
	case "in_progress":
		return fmt.Sprintf("🔄 Started: %s", formatTodoResponseForID(title, id))
	case "completed":
		if remaining == 0 {
			return fmt.Sprintf("🎉 Completed: %s - All todos done!", title)
		}
		return fmt.Sprintf("✅ Completed: %s (%d remaining)", formatTodoResponseForID(title, id), remaining)
	case "cancelled":
		return fmt.Sprintf("❌ Cancelled: %s", formatTodoResponseForID(title, id))
	default:
		return fmt.Sprintf("📝 Updated: %s → %s", formatTodoResponseForID(title, id), status)
	}
}

// formatBulkStatusSummary formats a summary of multiple status updates.
func formatBulkStatusSummary(updatedCount int, results []string) string {
	if updatedCount == 0 {
		return "No updates made"
	}
	if len(results) <= 3 {
		return strings.Join(results, ", ")
	}
	return fmt.Sprintf("Updated %d: %s, +%d more", updatedCount, results[0], len(results)-1)
}

// formatStatusWithID formats a single status change with ID for bulk summaries.
func formatStatusWithID(status, title, id string) string {
	symbol := getCompactStatusSymbol(status)
	return fmt.Sprintf("%s %s", symbol, formatTodoResponseForID(title, id))
}
