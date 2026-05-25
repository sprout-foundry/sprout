package agent

import (
	"context"
	"fmt"
	"strings"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// Tool handler implementations for todo and build validation operations

func handleTodoWrite(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	todosRaw, ok := args["todos"]
	if !ok {
		return "", agenterrors.NewInvalidInputError("missing todos argument", nil)
	}

	// Parse the todos array
	todosSlice, ok := todosRaw.([]interface{})
	if !ok {
		return "", agenterrors.NewInvalidInputError("todos must be an array", nil)
	}

	var todos []tools.TodoItem

	for _, todoRaw := range todosSlice {
		todoMap, ok := todoRaw.(map[string]interface{})
		if !ok {
			return "", agenterrors.NewInvalidInputError("each todo must be an object", nil)
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
			return "", agenterrors.NewInvalidInputError("each todo requires content", nil)
		}
		if todo.Status == "" {
			return "", agenterrors.NewInvalidInputError("each todo requires status", nil)
		}

		// Validate status against known values
		if !tools.IsValidStatus(todo.Status) {
			return "", agenterrors.NewInvalidInputError(
				fmt.Sprintf("todo %q: %s", todo.Content, tools.FormatTodoStatusError(todo.Status)), nil)
		}

		// Validate priority against known values (priority is optional, empty is allowed)
		if !tools.IsValidPriority(todo.Priority) {
			return "", agenterrors.NewInvalidInputError(
				fmt.Sprintf("todo %q: %s", todo.Content, tools.FormatTodoPriorityError(todo.Priority)), nil)
		}

		todos = append(todos, todo)
	}

	a.Logger().Debug("TodoWrite: processing %d todos\n", len(todos))
	result := a.GetTodoManager().Write(todos)
	a.Logger().Debug("TodoWrite result: %s\n", result)
	return result, nil
}

func handleTodoRead(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	a.Logger().Debug("TodoRead: returning current todo list\n")
	todos := a.GetTodoManager().Read()
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
