// Extracted from tool_security.go — path-related security helpers (SP-098).

package agent

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/sprout-foundry/sprout/pkg/clihooks"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
	"github.com/sprout-foundry/sprout/pkg/security"
	"github.com/sprout-foundry/sprout/pkg/utils"
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

// handleFileSecurityError checks if an error is due to filesystem security and prompts the user.
// Returns a context with security bypass enabled if user approves, original context otherwise.
//
// `resolvedPath` is the canonical target after symlink resolution (the
// actual filesystem object that would be touched). When non-empty, it
// is shown alongside the user-supplied `filePath` in the approval
// dialog so the user can verify the destination is what they expect.
// Pass "" when the caller cannot compute it; display falls back to
// `filePath` alone.
func handleFileSecurityError(ctx context.Context, agent *Agent, toolName, filePath, resolvedPath string, err error) (context.Context, bool) {
	// Check if this is a filesystem security error
	if !errors.Is(err, filesystem.ErrOutsideWorkingDirectory) && !errors.Is(err, filesystem.ErrWriteOutsideWorkingDirectory) {
		return ctx, false
	}

	// Unsafe mode bypasses filesystem security checks automatically
	if agent.GetUnsafeMode() {
		agent.debugLog("[UNLOCK] Unsafe mode: automatically allowing file access outside working directory: %s\n", filePath)
		return filesystem.WithSecurityBypass(ctx), true
	}

	// Session elevation (user clicked "Elevate (session)" on a prior
	// approval) bypasses filesystem prompts for non-sensitive paths.
	// Sensitive-tier paths (system dirs, off-CWD home) still prompt
	// even under elevation — they're never session-allowlisted.
	if agent.IsSessionElevated() {
		tier := ClassifyPathAccess(filePath, agent.GetWorkspaceRoot(), detectHomeDir(), agent.effectiveCwd())
		if tier != PathTierSensitive {
			agent.debugLog("[UNLOCK] Session elevated: automatically allowing file access outside working directory: %s (tier=%s)\n", filePath, tier)
			return filesystem.WithSecurityBypass(ctx), true
		}
		// Sensitive path under elevation: fall through to normal prompt.
		agent.debugLog("[APPROVAL] Sensitive-tier path still prompts under elevation: %s\n", filePath)
	}

	// Per-folder session allowlist short-circuit. If this path sits
	// under a folder the user previously approved, skip the prompt.
	if agent.IsFolderSessionAllowed(filePath) {
		// SP-128-1f: read_only declared paths still satisfy
		// IsFolderSessionAllowed (so reads continue to work), but
		// a write tool must NOT be allowed under a read_only grant.
		// We detect "write" via the error sentinel — every write
		// tool surfaces ErrWriteOutsideWorkingDirectory; read tools
		// surface ErrOutsideWorkingDirectory. This is the same
		// signal the rest of the function uses (see the first
		// errors.Is check at the top of this function), so the
		// classification stays consistent. When the path is on the
		// allowlist but the mode says read_only, return (ctx, false)
		// so the caller returns a workflow-specific error instead
		// of the generic off-workspace sentinel.
		if errors.Is(err, filesystem.ErrWriteOutsideWorkingDirectory) && !agent.IsFolderSessionWriteAllowed(filePath) {
			agent.debugLog("[APPROVAL] write blocked: %s is declared read_only in the active workflow's allowed_paths; filesystem gate refuses to authorize write\n", filePath)
			// Return a workflow-specific error so the caller surfaces the
			// workflow-specific message instead of the generic off-workspace
			// sentinel. errors.Is still matches via %w wrapping.
			return ctx, false
		}
		agent.debugLog("[UNLOCK] Folder is on session allowlist: %s\n", filePath)
		return filesystem.WithSecurityBypass(ctx), true
	}

	// Classify the path so we can pick the right dialog mode and
	// scope. Sensitive (system dirs, off-CWD home) gets 2 options;
	// External gets 3 options including "Allow folder this session".
	tier := ClassifyPathAccess(filePath, agent.GetWorkspaceRoot(), detectHomeDir(), agent.effectiveCwd())
	folder := filepath.Dir(filePath)

	// Display target = the user-typed path, with the canonical target
	// appended when it diverges (i.e. when filePath is a symlink).
	// Without this, a workspace symlink to /etc/passwd would prompt
	// "Allow access to workspace/link?" and approval would silently
	// widen access to /etc/passwd.
	displayPath := filePath
	if resolvedPath != "" && resolvedPath != filePath {
		displayPath = filePath + "\n   (resolves to: " + resolvedPath + ")"
	}

	// Subagents cannot prompt — return unapproved so the error propagates
	if agent.IsSubagent() {
		agent.debugLog("Subagent encountered filesystem security error for %s, delegating to primary agent\n", filePath)
		return ctx, false
	}

	// Prefer webui approval path when a browser tab is connected.
	if mgr := agent.GetSecurityApprovalMgr(); mgr != nil && agent.GetEventBus() != nil && agent.HasActiveWebUIClients() {
		// Suspend CLI spinner and steer reader before blocking on the
		// webui response — same rationale as the tool approval path above.
		clihooks.SuspendIndicator()
		clihooks.PauseSteer()
		defer clihooks.ResumeSteer()

		kind := "fs_external"
		if tier == PathTierSensitive {
			kind = "fs_sensitive"
		}
		prompt := fmt.Sprintf("The tool '%s' is attempting to access a file outside the working directory.", toolName)
		extras := map[string]string{
			"risk_type": "Filesystem Security",
			"target":    displayPath,
			"path":      displayPath,
			"kind":      kind,
		}
		if resolvedPath != "" && resolvedPath != filePath {
			extras["resolved_path"] = resolvedPath
		}
		if tier == PathTierExternal {
			extras["folder"] = folder
		}
		decision := mgr.RequestToolApprovalDecision(agent.GetEventBus(), agent.GetEventClientID(), agent.GetEventUserID(), toolName, "CAUTION", prompt, extras)
		return applyFilesystemDecision(ctx, agent, decision, filePath, folder, tier)
	}

	// CLI: prompt user interactively via terminal stdin
	agentConfig := agent.GetConfig()
	logger := utils.GetLogger(agentConfig != nil && agentConfig.SkipPrompt)
	canPrompt := logger != nil && logger.IsInteractive()

	if canPrompt {
		promptTier := utils.FilesystemPromptExternal
		if tier == PathTierSensitive {
			promptTier = utils.FilesystemPromptSensitive
		}
		// No leading glyph — the picker renderer prepends the ⚠ (avoids "⚠ ⚠").
		prompt := fmt.Sprintf("Filesystem Security Warning\n\nThe tool '%s' is attempting to access a file outside the working directory.", toolName)
		choice := logger.AskForFilesystemApproval(prompt, displayPath, folder, promptTier)
		decision := filesystemDecisionFromCLIChoice(choice)
		return applyFilesystemDecision(ctx, agent, decision, filePath, folder, tier)
	}

	// No prompting available — return unapproved
	if agent.debug {
		agent.debugLog("Cannot prompt for filesystem security approval (no mechanism): %s\n", filePath)
	}
	return ctx, false
}

// filesystemDecisionFromCLIChoice maps the CLI prompt's typed choice
// to the shared security.ApprovalDecision so the post-prompt handling
// is the same regardless of input surface.
func filesystemDecisionFromCLIChoice(c utils.ApprovalChoice) security.ApprovalDecision {
	switch c {
	case utils.ApprovalChoiceApproveOnce:
		return security.ApprovalApproveOnce
	case utils.ApprovalChoiceAllowFolderSession:
		return security.ApprovalAllowFolderSession
	default:
		return security.ApprovalDeny
	}
}

// applyFilesystemDecision performs the side effects of the user's
// choice on a filesystem approval dialog and returns the (ctx, ok)
// pair the caller (handleFileSecurityError) returns to the tool layer.
//
//   - ApprovalDeny → reject (no ctx mutation).
//   - ApprovalApproveOnce → allow this invocation only (ctx gets the
//     bypass token; nothing recorded for future calls).
//   - ApprovalAllowFolderSession → External tier only: add the folder
//     to the agent's session allowlist AND allow this invocation.
//     Silently demoted to ApproveOnce if the tier is Sensitive (the
//     dialog shouldn't have offered the choice, but defense-in-depth).
//
// Decisions intended for the shell flow (ApproveAlways, Elevate)
// don't apply here; if encountered they collapse to ApproveOnce so
// the invocation still proceeds and no shell-specific side effect
// fires for a filesystem operation.
func applyFilesystemDecision(ctx context.Context, agent *Agent, decision security.ApprovalDecision, filePath, folder string, tier PathTier) (context.Context, bool) {
	switch decision {
	case security.ApprovalDeny:
		agent.debugLog("[APPROVAL] User denied file access outside working directory: %s\n", filePath)
		return ctx, false
	case security.ApprovalAllowFolderSession:
		if tier == PathTierSensitive {
			// Sensitive paths can never be allowlisted. If we got
			// this decision anyway (broken client / API misuse),
			// treat it as a one-shot approval so the user isn't
			// silently widened.
			agent.debugLog("[APPROVAL] Refusing to allowlist Sensitive tier path %s; demoting to one-shot approval\n", filePath)
			return filesystem.WithSecurityBypass(ctx), true
		}
		agent.debugLog("[APPROVAL] User approved folder %s for the rest of this session (path: %s)\n", folder, filePath)
		agent.AddSessionAllowedFolder(folder)
		return filesystem.WithSecurityBypass(ctx), true
	case security.ApprovalElevate:
		// User clicked "Elevate (session)" on a filesystem dialog.
		// Apply the session-wide elevation so ALL subsequent gates
		// (static classifier, filesystem, shell cascade) skip prompts.
		agent.ElevateSessionToPermissive()
		agent.debugLog("[APPROVAL] Session elevated from filesystem dialog for: %s\n", filePath)
		return filesystem.WithSecurityBypass(ctx), true
	default:
		// ApprovalApproveOnce + any other decision collapses to a
		// single-invocation approval.
		agent.debugLog("[APPROVAL] User approved file access (one-shot): %s\n", filePath)
		return filesystem.WithSecurityBypass(ctx), true
	}
}
