package tools

import (
	"context"
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/events"
)

type repoMapHandler struct{}

func (h *repoMapHandler) Name() string {
	return "repo_map"
}

func (h *repoMapHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "repo_map",
					Description: "Generate a lightweight overview of the codebase showing file paths and top-level symbols (functions, types, interfaces, classes) with line numbers. Use this before reading files to identify which files and functions are relevant to your task. Output is limited to ~1024 tokens. Supports Go, TypeScript, JavaScript, Python, Rust, Java, and C files.",Parameters: []ParameterDef{
			{Name: "directory", Type: "string", Description: "Directory to scan (default: .)"},
		},
		Required: []string{},
	}
}

func (h *repoMapHandler) Validate(args map[string]any) error {
	return nil
}

func (h *repoMapHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	toolName := h.Name()
	if env.EventBus != nil {
		env.EventBus.Publish(events.EventTypeToolStart, map[string]any{
			"tool":     toolName,
			"params":   args,
		})
		defer func() {
			env.EventBus.Publish(events.EventTypeToolEnd, map[string]any{
				"tool":  toolName,
				"error": false,
			})
		}()

		directory, _ := extractString(args, "directory")
		if directory == "" {
			directory = "."
		}

		output, err := GenerateRepoMap(ctx, directory)
		if err != nil {
			return ToolResult{
				Output:  fmt.Sprintf("Error generating repo map: %v", err),
				IsError: true,
			}, nil
		}

		return ToolResult{
			Output:    output,
			IsError:   false,
		}, nil
	}

	return ToolResult{
		Output:  "No results",
		IsError: false,
	}, nil
}
