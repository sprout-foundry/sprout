package tools

import (
	"context"
	"fmt"
	"time"
)

type repoMapHandler struct{}

func (h *repoMapHandler) Name() string {
	return "repo_map"
}

func (h *repoMapHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "repo_map",
		Description: "Generate a lightweight overview of the codebase showing file paths and top-level symbols (functions, types, interfaces, classes) with line numbers. Use this before reading files to identify which files and functions are relevant to your task. Output is limited to ~1024 tokens. Supports Go, TypeScript, JavaScript, Python, Rust, Java, and C files.", Parameters: []ParameterDef{
			{Name: "directory", Type: "string", Description: "Directory to scan (default: .)"},
		},
		Required: []string{},
	}
}

func (h *repoMapHandler) Validate(args map[string]any) error {
	return nil
}

func (h *repoMapHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
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
		Output:  output,
		IsError: false,
	}, nil
}

func (h *repoMapHandler) Aliases() []string      { return nil }
func (h *repoMapHandler) Timeout() time.Duration { return 0 }
func (h *repoMapHandler) MaxResultSize() int     { return 0 }
func (h *repoMapHandler) SafeForParallel() bool  { return false }
func (h *repoMapHandler) Interactive() bool      { return false }
