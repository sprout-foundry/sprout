package tools

import (
	"context"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// RecoverFileFunc is a function pointer set by pkg/agent at startup.
// It bridges the new ToolHandler interface with the legacy handleRecoverFile
// implementation that requires *Agent access.
//
// The function signature matches the legacy handler:
//
//	handleRecoverFile(ctx, args) → JSON string
//
// The agent sets this pointer during initialization, capturing the *Agent
// reference in a closure so the handler doesn't need direct access.
//
// Phase 4 of SP-109 will migrate the execute logic into this package,
// eliminating the need for this indirection.
var RecoverFileFunc func(ctx context.Context, args map[string]any) (string, error)

// recoverFileHandler implements ToolHandler for the recover_file tool.
// It restores one file (or one bulk entry's worth of files) from the
// ChangeTracker's session buffer.
//
// This is a THIN WRAPPER that delegates Execute to the function pointer
// RecoverFileFunc. All metadata (Name, Definition, Validate, Aliases,
// Timeout, etc.) lives here.
type recoverFileHandler struct{}

func (h *recoverFileHandler) Name() string { return "recover_file" }

func (h *recoverFileHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "recover_file",
		Description: "Restore one file (or one bulk entry's worth of files) from the ChangeTracker's session buffer. " +
			"Works for any tool's edits — write_file, edit_file, or shell_command (rm, sed -i, mv, `git checkout .`).\n\n" +
			"**`scope`**:\n" +
			"• `\"latest\"` (default) — restore to the state immediately before the most recent tracked change for `path`. Undoes one specific edit.\n" +
			"• `\"session_start\"` — restore to the state before the agent first touched this file this session. Use when the file went through multiple edits.\n" +
			"• `\"bulk\"` — treat `path` as the bulk entry's `path` from list_changes (e.g. \"git checkout .\"). Restores every packed file. Use to undo a high-volume destructive command.\n\n" +
			"**Per-op (single-file scopes)**: edit/modified → write originals back; delete → un-delete; create → remove the file.\n\n" +
			"When scope is `\"latest\"` or `\"session_start\"` and `path` was packed into a bulk entry, recover_file finds it inside the bulk and restores just that one file.\n\n" +
			"**Returns**: single-file scopes → `{recovered, path, action, message}`. Bulk scope → `{found, bulk_path, restored, failed, summary, entries[]}`.\n\n" +
			"**Safety**: only files the tracker recorded can be recovered — call list_changes first. Files with `recoverable: false` (binary, >1 MiB, outside workspace) cannot be restored. Bulk entries recorded as count-only (memory cap exceeded) cannot be bulk-recovered.",
		Required: []string{"path"},
		Parameters: []ParameterDef{
			{Name: "path", Type: "string", Required: true, Description: "Absolute or relative path to the file to recover. For scope=\"bulk\", use the bulk entry's `path` field from list_changes."},
			{Name: "scope", Type: "string", Description: "\"latest\" (default), \"session_start\", or \"bulk\". See description for semantics."},
		},
	}
}

func (h *recoverFileHandler) Validate(args map[string]any) error {
	_, err := extractString(args, "path")
	return err
}

func (h *recoverFileHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
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

	if RecoverFileFunc == nil {
		hadError = true
		return ToolResult{
			Output:  "recover_file is not available: agent integration not initialized (RecoverFileFunc is nil)",
			IsError: true,
		}, nil
	}

	result, err := RecoverFileFunc(ctx, args)
	if err != nil {
		hadError = true
		return ToolResult{Output: err.Error(), IsError: true}, nil
	}
	return ToolResult{Output: result}, nil
}

func (h *recoverFileHandler) Aliases() []string         { return nil }
func (h *recoverFileHandler) Timeout() time.Duration    { return 0 }
func (h *recoverFileHandler) MaxResultSize() int        { return 0 }
func (h *recoverFileHandler) SafeForParallel() bool     { return false }
func (h *recoverFileHandler) Interactive() bool         { return false }
