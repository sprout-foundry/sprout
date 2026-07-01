package tools

import (
	"context"
	"fmt"
	"strings"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/events"
)

type todoReadHandler struct{}

func (h *todoReadHandler) Name() string { return "todo_read" }

func (h *todoReadHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "todo_read",
		Description: "Read the current to-do list for the session.",
		Required:    []string{},
		Parameters:  []ParameterDef{},
	}
}

func (h *todoReadHandler) Validate(args map[string]any) error {
	if args == nil || len(args) == 0 {
		return agenterrors.NewValidation("arguments must not be nil or empty", nil)
	}
	return nil
}

func (h *todoReadHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
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

	todos := TodoRead()
	if len(todos) == 0 {
		return ToolResult{Output: "No todos"}, nil
	}

	var sb strings.Builder
	for _, todo := range todos {
		status := todo.Status
		if status == "in_progress" {
			status = "active"
		}
		sb.WriteString(fmt.Sprintf("- [%s] %s\n", status[:1], todo.Content))
	}
	return ToolResult{Output: sb.String()}, nil
}
