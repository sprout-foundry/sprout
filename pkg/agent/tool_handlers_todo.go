package agent

import (
	"context"
	"fmt"
	"strings"

	tools "github.com/alantheprice/ledit/pkg/agent_tools"
)

// Tool handler implementations for todo and build validation operations

func handleTodoWrite(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	todosRaw, ok := args["todos"]
	if !ok {
		return "", fmt.Errorf("missing todos argument")
	}

	// Parse the todos array
	todosSlice, ok := todosRaw.([]interface{})
	if !ok {
		return "", fmt.Errorf("todos must be an array")
	}

	var todos []tools.TodoItem

	for _, todoRaw := range todosSlice {
		todoMap, ok := todoRaw.(map[string]interface{})
		if !ok {
			return "", fmt.Errorf("each todo must be an object")
		}

		todo := tools.TodoItem{}

		if content, ok := todoMap["content"].(string); ok {
			todo.Content = content
		}
		if status, ok := todoMap["status"].(string); ok {
			todo.Status = status
		}
		if priority, ok := todoMap["priority"].(string); ok {
			todo.Priority = priority
		}
		if id, ok := todoMap["id"].(string); ok {
			todo.ID = id
		}

		if todo.Content == "" {
			return "", fmt.Errorf("each todo requires content")
		}
		if todo.Status == "" {
			return "", fmt.Errorf("each todo requires status")
		}
		todos = append(todos, todo)
	}

	a.debugLog("TodoWrite: processing %d todos\n", len(todos))
	result := tools.TodoWrite(todos)
	a.debugLog("TodoWrite result: %s\n", result)
	return result, nil
}

func handleTodoRead(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	a.debugLog("TodoRead: returning current todo list\n")
	todos := tools.TodoRead()
	if len(todos) == 0 {
		return "No todos", nil
	}

	var result strings.Builder
	for _, todo := range todos {
		status := todo.Status
		if status == "in_progress" {
			status = "active"
		}
		result.WriteString(fmt.Sprintf("- [%s] %s\n", status[:1], todo.Content))
	}
	return result.String(), nil
}
