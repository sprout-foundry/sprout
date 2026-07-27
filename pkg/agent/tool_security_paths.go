// Extracted from tool_security.go — path-related security helpers (SP-098).

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// fileTouchingTools is the set of tool names that carry a "path" argument.
// Used by extractFilePathAndMode to determine whether to supply path context
// to staticGateAutoApprove.
var fileTouchingTools = map[string]bool{
	"read_file":             true,
	"write_file":            true,
	"edit_file":             true,
	"write_structured_file": true,
	"patch_structured_file": true,
	"list_directory":        true,
}

// extractFilePathAndMode returns the file path and access mode for a tool call,
// or ("", "") for non-file tools. Path is the "path" argument value; mode is
// "write" for write/edit tools, "read" for read tools. Non-file tools and tools
// that don't supply a "path" arg return the zero values so the classifier skips
// the path-tier branch.
//
// Path resolution convention for callers:
//   - filePath is the user-supplied path (may be relative or absolute).
//   - resolvedPath is the symlink-evaluated canonical target (empty if the path
//     does not exist or the caller did not perform resolution).
//   - When resolvedPath is non-empty, classifyFileAccess uses it for workspace
//     containment and sensitive-path checks, falling back to filePath if the
//     resolved target does not exist.
//   - When resolvedPath is empty, the function uses filePath directly for the
//     prefix checks — relative paths that don't exist are evaluated lexically.
func extractFilePathAndMode(toolName string, args map[string]interface{}) (filePath, mode string) {
	if !fileTouchingTools[toolName] {
		return "", ""
	}
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return "", ""
	}
	switch toolName {
	case "write_file", "edit_file", "write_structured_file", "patch_structured_file":
		return path, "write"
	default:
		return path, "read"
	}
}

// FileAccessDecision describes the resolved verdict for a file-path operation
// from Gate 1's path-tier classifier.
type FileAccessDecision int

const (
	// FileAccessAllow: path is in an allowlisted location (workspace root,
	// session-allowlisted folder, or /tmp).
	FileAccessAllow FileAccessDecision = iota
	// FileAccessPrompt: path is outside the allowlist and not hard-blocked;
	// user must approve.
	FileAccessPrompt
	// FileAccessDeny: path targets a known hard-block location or violates
	// a declared read_only constraint.
	FileAccessDeny
)

// classifyFileAccess inspects the path tier and returns the access verdict.
// Used by both Gate 1 (staticGateAutoApprove) and the filesystem gate adapter
// so they always agree on allow/prompt/deny for a given path.
//
// Inputs:
//   - filePath: the user-supplied path (may be relative, may not exist)
//   - resolvedPath: the symlink-evaluated canonical form (may equal filePath)
//   - mode: "read" or "write" (controls which allowlists apply)
//
// Returns:
//   - FileAccessAllow when the path lands in workspace root, session-allowlisted
//     folder, /tmp, or another gate-bypass-visible location.
//   - FileAccessPrompt when the path is outside the allowlist and not hard-blocked.
//   - FileAccessDeny when the path targets a known hard-block location or when
//     a write is attempted against a read_only declared allowlist entry.
func (a *Agent) classifyFileAccess(filePath, resolvedPath, mode string) FileAccessDecision {
	if a == nil {
		return FileAccessPrompt
	}
	// Use resolvedPath when available; fall back to filePath for lexical checks.
	target := resolvedPath
	if target == "" {
		target = filePath
	}
	// /tmp is universally allowed regardless of mode.
	if filesystem.IsUnderTmpPath(target) {
		return FileAccessAllow
	}
	// Workspace root and subdirectories are always allowed.
	if a.IsUnderWorkspaceRoot(target) {
		return FileAccessAllow
	}
	// Session-allowlisted folders (workflow-declared allowed_paths + user
	// mid-session approvals) are allowed subject to their declared mode.
	if a.IsFolderSessionAllowed(target) {
		if mode == "write" && a.IsReadOnlyAllowedFolder(target) {
			return FileAccessDeny
		}
		return FileAccessAllow
	}
	// Sensitive system paths always prompt rather than hard-deny.
	// The user must confirm access explicitly.
	if filesystem.IsSensitiveSystemPath(target) {
		return FileAccessPrompt
	}
	return FileAccessPrompt
}

// ClassifyFileAccess implements tools.FileAccessClassifier so handlers
// can consult Gate 1's path-tier verdict without importing pkg/agent.
// Translates the internal FileAccessDecision enum to the interface's
// string contract: "allow", "prompt", "deny". Logs the verdict to the
// audit logger on ctx (SP-127 M3.2 Phase 2.6 follow-on) so every
// decision appears in the audit trail.
func (a *Agent) ClassifyFileAccess(ctx context.Context, filePath, resolvedPath, mode string) string {
	decision := a.classifyFileAccess(filePath, resolvedPath, mode)
	switch decision {
	case FileAccessAllow:
		a.auditPathDecision(ctx, filePath, resolvedPath, mode, "allowed", "low")
		return "allow"
	case FileAccessDeny:
		a.auditPathDecision(ctx, filePath, resolvedPath, mode, "denied", "high")
		return "deny"
	default:
		a.auditPathDecision(ctx, filePath, resolvedPath, mode, "prompted", "medium")
		return "prompt"
	}
}