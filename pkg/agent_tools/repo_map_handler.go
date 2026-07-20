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
		Description: "Generate a lightweight overview of the codebase showing file paths and top-level symbols (functions, types, interfaces, classes) with line numbers. Use this before reading files to identify which files and functions are relevant to your task. Supports Go, TypeScript, JavaScript, Python, Rust, Java, and C files.", Parameters: []ParameterDef{
			{Name: "directory", Type: "string", Description: "Directory to scan (default: .)"},
			{Name: "depth", Type: "integer", Description: "Detail level: 1=directory tree only, 2=tree+top-level symbols, 3=full symbols (default)"},
			{Name: "query", Type: "string", Description: "Filter to files whose path or symbols contain this string (case-insensitive)"},
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

	depth := env.RepoMapDefaultDepth // SP-125: profile override (1 in LCM, 0 = default 3)
	if depth <= 0 {
		depth = 3 // default: full symbols
	}
	if d, ok := args["depth"]; ok {
		switch v := d.(type) {
		case int:
			depth = v
		case int64:
			depth = int(v)
		case float64:
			depth = int(v)
		}
	}

	query, _ := extractString(args, "query")

	output, err := GenerateRepoMap(ctx, directory, depth, query)
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
