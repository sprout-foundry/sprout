package tools

import (
	"context"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// ListChangesFunc is a function pointer set by pkg/agent at startup.
// It bridges the new ToolHandler interface with the legacy handleListChanges
// implementation that requires *Agent access.
//
// The function signature matches the legacy handler:
//
//	handleListChanges(ctx, args) → JSON string
//
// The agent sets this pointer during initialization, capturing the *Agent
// reference in a closure so the handler doesn't need direct access.
//
// Phase 4 of SP-109 will migrate the execute logic into this package,
// eliminating the need for this indirection.
var ListChangesFunc func(ctx context.Context, args map[string]any) (string, error)

// listChangesHandler implements ToolHandler for the list_changes tool.
// It lists files created, modified, or deleted during the current session
// with optional knobs for diffs, activity-block summaries, and persisted
// history merging.
//
// This is a THIN WRAPPER that delegates Execute to the function pointer
// ListChangesFunc. All metadata (Name, Definition, Validate, Aliases,
// Timeout, etc.) lives here.
type listChangesHandler struct{}

func (h *listChangesHandler) Name() string { return "list_changes" }

func (h *listChangesHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "list_changes",
		Description: "List files created, modified, or deleted this session. " +
			"Use for commit messages, progress checks, and cross-session reasoning. " +
			"Supports diffs, activity-block grouping, and persisted history merge.",
		Required: []string{},
		Parameters: []ParameterDef{
			{Name: "since", Type: "string", Description: "RFC3339 timestamp or duration (2d, 12h, 30m) cutoff"},
			{Name: "tool", Type: "string", Description: "Tool name filter (e.g. write_file, edit_file)"},
			{Name: "path_pattern", Type: "string", Description: "Path glob filter (e.g. pkg/auth/*.go)"},
			{Name: "include_diff", Type: "boolean", Description: "Add per-file unified diff to results"},
			{Name: "group_by", Type: "string", Description: "Set to 'block' for activity-block summary"},
			{Name: "include_persisted", Type: "boolean", Description: "Merge in persistent history records"},
		},
	}
}

func (h *listChangesHandler) Validate(args map[string]any) error {
	// All parameters are optional; no validation needed.
	return nil
}

func (h *listChangesHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	toolName := h.Name()
	var hadError bool
	if env.EventBus != nil {
		env.EventBus.Publish(events.EventTypeToolStart, map[string]any{
			"tool":   toolName,
			"params": args,
		})
		defer func() {
			env.EventBus.Publish(events.EventTypeToolEnd, map[string]any{
				"tool":  toolName,
				"error": hadError,
			})
		}()
	}

	if ListChangesFunc == nil {
		hadError = true
		return ToolResult{
			Output:  "list_changes is not available: agent integration not initialized (ListChangesFunc is nil)",
			IsError: true,
		}, nil
	}

	result, err := ListChangesFunc(ctx, args)
	if err != nil {
		hadError = true
		return ToolResult{Output: err.Error(), IsError: true}, nil
	}
	return ToolResult{Output: result}, nil
}

func (h *listChangesHandler) Aliases() []string         { return nil }
func (h *listChangesHandler) Timeout() time.Duration    { return 0 }
func (h *listChangesHandler) MaxResultSize() int        { return 0 }
func (h *listChangesHandler) SafeForParallel() bool     { return false }
func (h *listChangesHandler) Interactive() bool         { return false }
