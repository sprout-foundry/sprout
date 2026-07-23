package tools

import (
	"context"
	"time"
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
		Description: "Restore one file from the ChangeTracker's session buffer. " +
			"Scopes: 'latest' (undo last edit), 'session_start' (pre-session state), 'bulk' (undo bulk ops like git checkout).",
		Required: []string{"path"},
		Parameters: []ParameterDef{
			{Name: "path", Type: "string", Required: true, Description: "Path to recover. For bulk scope, use the bulk entry path from list_changes"},
			{Name: "scope", Type: "string", Description: "'latest' (default), 'session_start', or 'bulk'"},
		},
	}
}

func (h *recoverFileHandler) Validate(args map[string]any) error {
	_, err := extractString(args, "path")
	return err
}

func (h *recoverFileHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	ToolFuncMu.RLock()
	fn := RecoverFileFunc
	ToolFuncMu.RUnlock()
	if fn == nil {
		return ToolResult{
			Output:  "recover_file is not available: agent integration not initialized (RecoverFileFunc is nil)",
			IsError: true,
		}, nil
	}

	result, err := fn(ctx, args)
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, nil
	}
	return ToolResult{Output: result}, nil
}

func (h *recoverFileHandler) Aliases() []string      { return nil }
func (h *recoverFileHandler) Timeout() time.Duration { return 0 }
func (h *recoverFileHandler) MaxResultSize() int     { return 0 }
func (h *recoverFileHandler) SafeForParallel() bool  { return false }
func (h *recoverFileHandler) Interactive() bool      { return false }
