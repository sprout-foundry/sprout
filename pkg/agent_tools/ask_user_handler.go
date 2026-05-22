package tools

import (
	"context"
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/events"
)

type askUserHandler struct{}

func (h *askUserHandler) Name() string { return "ask_user" }

func (h *askUserHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "ask_user",
		Description: "Ask the user a question and wait for their response. Use this when you need clarification, user input, or a decision that cannot be determined from context alone.",
		Required: []string{"question"},
		Parameters: []ParameterDef{
			{Name: "question", Type: "string", Required: true, Description: "The question to ask the user (required)"},
		},
	}
}

func (h *askUserHandler) Validate(args map[string]any) error {
	_, err := extractString(args, "question")
	return err
}

func (h *askUserHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
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

	question, _ := extractString(args, "question")

	// ask_user in the new handler pattern supports CLI mode directly.
	// WebUI mode requires *Agent access for event bus routing and ask_user manager,
	// which is not available in the ToolEnv interface. CLI mode works fine.
	response, err := AskUser(question)
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("ask_user failed: %v", err), IsError: true}, nil
	}
	return ToolResult{Output: response}, nil
}
