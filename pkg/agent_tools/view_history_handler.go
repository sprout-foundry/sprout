package tools

import (
	"context"
	"fmt"
	"time"
)

type viewHistoryHandler struct{}

func (h *viewHistoryHandler) Name() string {
	return "view_history"
}

func (h *viewHistoryHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "view_history",
		Description: "View recent change history tracked by the agent",
		Parameters: []ParameterDef{
			{Name: "limit", Type: "integer", Description: "Maximum number of entries to return (default 10)"},
			{Name: "file_filter", Type: "string", Description: "Filter by filename (partial match)"},
			{Name: "since", Type: "string", Description: "Only include changes after this ISO 8601 timestamp"},
			{Name: "show_content", Type: "boolean", Description: "Include content summaries for each change"},
		},
		Required: []string{},
	}
}

func (h *viewHistoryHandler) Validate(args map[string]any) error {
	return nil
}

func (h *viewHistoryHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	limit, _ := extractInt(args, "limit")
	if limit <= 0 {
		limit = 10
	} else if limit > 100 {
		limit = 100
	}
	fileFilter, _ := extractString(args, "file_filter")
	sinceStr, _ := extractString(args, "since")
	showContent := getBoolArg(args, "show_content")

	var sinceTime *time.Time
	if sinceStr != "" {
		t, err := time.Parse(time.RFC3339, sinceStr)
		if err != nil {
			return ToolResult{
				Output:  fmt.Sprintf("Error parsing 'since' timestamp: %v. Use ISO 8601 format (e.g., 2024-01-01T00:00:00Z)", err),
				IsError: true,
			}, nil
		}
		sinceTime = &t
	}

	result, err := ViewHistory(limit, fileFilter, sinceTime, showContent)
	if err != nil {
		return ToolResult{
			Output:  fmt.Sprintf("Error viewing history: %v", err),
			IsError: true,
		}, nil
	}

	return ToolResult{
		Output:        result.Output,
		IsError:       false,
		StructuredOut: result.Metadata,
	}, nil
}

func (h *viewHistoryHandler) Aliases() []string      { return nil }
func (h *viewHistoryHandler) Timeout() time.Duration { return 0 }
func (h *viewHistoryHandler) MaxResultSize() int     { return 0 }
func (h *viewHistoryHandler) SafeForParallel() bool  { return false }
func (h *viewHistoryHandler) Interactive() bool      { return false }
