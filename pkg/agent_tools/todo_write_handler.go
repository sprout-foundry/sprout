package tools

import (
	"context"
	"fmt"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/events"
)

type todoWriteHandler struct{}

func (h *todoWriteHandler) Name() string { return "todo_write" }

func (h *todoWriteHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "todo_write",
		Description: "Create and manage a structured task list for the current coding session.",
		Required:    []string{"todos"},
		Parameters: []ParameterDef{
			{Name: "todos", Type: "array", Required: true, Description: "Array of todo items: [{content, status, activeForm?, priority?, id?}]. `content` is the imperative task description; `activeForm` is the present-continuous phrasing shown in the activity indicator while in_progress (e.g. content \"Implement X\" → activeForm \"Implementing X\"). `priority` is high/medium/low (visual hint only)."},
		},
	}
}

func (h *todoWriteHandler) Validate(args map[string]any) error {
	todosRaw, ok := args["todos"]
	if !ok {
		return agenterrors.NewValidation("parameter 'todos' is required", nil)
	}
	todosSlice, ok := todosRaw.([]interface{})
	if !ok {
		return agenterrors.NewValidation("parameter 'todos' must be an array", nil)
	}
	for i, todoRaw := range todosSlice {
		todoMap, ok := todoRaw.(map[string]interface{})
		if !ok {
			return agenterrors.NewValidation(fmt.Sprintf("each todo must be an object, got %T at index %d", todoRaw, i), nil)
		}
		if content, ok := todoMap["content"].(string); ok {
			if content == "" {
				return agenterrors.NewValidation(fmt.Sprintf("todo at index %d requires non-empty 'content'", i), nil)
			}
		} else {
			return agenterrors.NewValidation(fmt.Sprintf("todo at index %d requires 'content' string", i), nil)
		}
		if status, ok := todoMap["status"].(string); ok {
			if !IsValidStatus(status) {
				return agenterrors.NewValidation(fmt.Sprintf("todo at index %d: %s", i, FormatTodoStatusError(status)), nil)
			}
		} else {
			return agenterrors.NewValidation(fmt.Sprintf("todo at index %d requires 'status' string", i), nil)
		}
		if priority, ok := todoMap["priority"].(string); ok {
			if !IsValidPriority(priority) {
				return agenterrors.NewValidation(fmt.Sprintf("todo at index %d: %s", i, FormatTodoPriorityError(priority)), nil)
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
	todos := make([]TodoItem, 0, len(todosRaw))
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
		if activeForm, ok := todoMap["activeForm"].(string); ok {
			todo.ActiveForm = activeForm
		}
		todos = append(todos, todo)
	}

	mgr := env.TodoManager
	if mgr == nil {
		mgr = ManagerForChat("")
	}
	result := mgr.Write(todos)

	if env.IsInteractiveCLI && env.OutputWriter != nil {
		RenderTodosForCLI(env.OutputWriter, todos)
	}

	return ToolResult{Output: result}, nil
}

func (h *todoWriteHandler) Aliases() []string         { return nil }
func (h *todoWriteHandler) Timeout() time.Duration    { return 0 }
func (h *todoWriteHandler) MaxResultSize() int        { return 0 }
func (h *todoWriteHandler) SafeForParallel() bool     { return false }
func (h *todoWriteHandler) Interactive() bool         { return false }
