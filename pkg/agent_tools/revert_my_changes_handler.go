package tools

import (
	"context"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// RevertMyChangesFunc is a function pointer set by pkg/agent at startup.
// It bridges the new ToolHandler interface with the legacy handleRevertMyChanges
// implementation that requires *Agent access.
//
// The function signature matches the legacy handler:
//
//	handleRevertMyChanges(ctx, args) → JSON string
//
// The agent sets this pointer during initialization, capturing the *Agent
// reference in a closure so the handler doesn't need direct access.
//
// Phase 4 of SP-109 will migrate the execute logic into this package,
// eliminating the need for this indirection.
var RevertMyChangesFunc func(ctx context.Context, args map[string]any) (string, error)

// revertMyChangesHandler implements ToolHandler for the revert_my_changes tool.
// It bulk-undoes session edits using the ChangeTracker's original content,
// restoring files to their pre-session state.
//
// This is a THIN WRAPPER that delegates Execute to the function pointer
// RevertMyChangesFunc. All metadata (Name, Definition, Validate, Aliases,
// Timeout, etc.) lives here.
type revertMyChangesHandler struct{}

func (h *revertMyChangesHandler) Name() string { return "revert_my_changes" }

func (h *revertMyChangesHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "revert_my_changes",
		Description: "Bulk-undo YOUR session edits using ChangeTracker originals — does NOT touch git or other agents' / user's in-progress work.\n\n" +
			"**Scope** (pick one):\n" +
			"• `scope=\"all\"` (default) — every file the tracker recorded this session.\n" +
			"• `since=<RFC3339 timestamp OR duration>` (e.g. \"30m\", \"2h\", \"2026-05-27T10:00:00Z\") — changes at/after that time.\n\n" +
			"Per file: edits → write originals back; deletes → un-delete; creates → remove the file. " +
			"Returns `{restored, failed, summary, entries: [{path, action, ok, message}]}`.\n\n" +
			"For SINGLE-file recovery use `recover_file` (`scope=\"session_start\"` for pre-session, `scope=\"latest\"` for last edit).\n\n" +
			"**Prefer this over `git checkout`/`git reset`** — git wipes EVERYTHING including the user's uncommitted work; this only touches files YOU edited.",
		Required: []string{},
		Parameters: []ParameterDef{
			{Name: "scope", Type: "string", Description: "\"all\" to revert every change this session. Default when no other filter is provided."},
			{Name: "since", Type: "string", Description: "RFC3339 timestamp or duration (30m, 2h, 2d) — revert all changes recorded at/after this moment."},
		},
	}
}

func (h *revertMyChangesHandler) Validate(args map[string]any) error {
	// All parameters are optional; no validation needed.
	return nil
}

func (h *revertMyChangesHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
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

	if RevertMyChangesFunc == nil {
		hadError = true
		return ToolResult{
			Output:  "revert_my_changes is not available: agent integration not initialized (RevertMyChangesFunc is nil)",
			IsError: true,
		}, nil
	}

	result, err := RevertMyChangesFunc(ctx, args)
	if err != nil {
		hadError = true
		return ToolResult{Output: err.Error(), IsError: true}, nil
	}
	return ToolResult{Output: result}, nil
}

func (h *revertMyChangesHandler) Aliases() []string         { return nil }
func (h *revertMyChangesHandler) Timeout() time.Duration    { return 0 }
func (h *revertMyChangesHandler) MaxResultSize() int        { return 0 }
func (h *revertMyChangesHandler) SafeForParallel() bool     { return false }
func (h *revertMyChangesHandler) Interactive() bool         { return false }
