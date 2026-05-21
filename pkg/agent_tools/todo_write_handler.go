package tools

import (
	"context"
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/events"
)

type todoWriteHandler struct{}

func (h *todoWriteHandler) Name() string { return "todo_write" }

func (h *todoWriteHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "todo_write",
		Description: "Create and manage a structured task list for the current coding session.",
		Required: []string{"todos"},
		Parameters: []ParameterDef{
			{Name: "todos", Type: "array", Required: true, Description: "Array of todo items: [{content, status, activeForm, priority, id}]"},
		},
	}
}

func (h *todoWriteHandler) Validate(args map[string]any) error {
	todosRaw, ok := args["todos"]
	if !ok {
		return fmt.Errorf("parameter 'todos' is required")
	}
	todosSlice, ok := todosRaw.([]interface{})
	if !ok {
		return fmt.Errorf("parameter 'todos' must be an array")
	}
	for i, todoRaw := range todosSlice {
		todoMap, ok := todoRaw.(map[string]interface{})
		if !ok {
			return fmt.Errorf("each todo must be an object, got %T at index %d", todoRaw, i)
		}
		if content, ok := todoMap["content"].(string); ok {
			if content == "" {
				return fmt.Errorf("todo at index %d requires non-empty 'content'", i)
			}
		} else {
			return fmt.Errorf("todo at index %d requires 'content' string", i)
		}
		if status, ok := todoMap["status"].(string); ok {
			if !IsValidStatus(status) {
				return fmt.Errorf("todo at index %d: %s", i, FormatTodoStatusError(status))
			}
		} else {
			return fmt.Errorf("todo at index %d requires 'status' string", i)
		}
		if priority, ok := todoMap["priority"].(string); ok {
			if !IsValidPriority(priority) {
				return fmt.Errorf("todo at index %d: %s", i, FormatTodoPriorityError(priority))
			}
		}
	}
	return nil
}

func (h *todoWriteHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	toolName := h.Name()
	if env.EventBus != nil {
		env.EventBus.Publish(events.EventTypeToolStart, map[string]any{
			"tool":   toolName,
			"params": args,
		})
		defer func() {
			env.EventBus.Publish(events.EventTypeToolEnd, map[string]any{
				"tool":  toolName,
				"error": false,
			})
		}()
	}

	todosRaw := args["todos"].([]interface{})
	var todos []TodoItem
	for _, todoRaw := range todosRaw {
		todoMap := todoRaw.(map[string]interface{})
		todo := TodoItem{}
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
		_ = todoMap["activeForm"] // acknowledged but not stored in TodoItem
		todos = append(todos, todo)
	}

	result := TodoWrite(todos)
	return ToolResult{Output: result}, nil
}
