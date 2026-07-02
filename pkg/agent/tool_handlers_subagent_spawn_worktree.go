// Subagent spawn worktree helpers: file path validation, external workspace
// approval, and workspace root override.
//
// Extracted from tool_handlers_subagent_spawn.go as part of SP-075's
// large-file decomposition.

package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// validateFilePaths resolves each file path to an absolute path and
// classifies them into workspace vs outside paths. Returns the absolute
// paths, the outside-only paths, and any error.
func validateFilePaths(files []string, absWorkspaceDir string, a *Agent) (absFilePaths, outsidePaths []string, _ error) {
	for _, filePath := range files {
		// Clean the path to eliminate any . or redundant separators
		cleanedPath := filepath.Clean(filePath)
		var absPath string
		if filepath.IsAbs(cleanedPath) {
			absPath = cleanedPath
		} else {
			// Resolve relative paths against the workspace root, not the process cwd
			absPath = filepath.Join(absWorkspaceDir, cleanedPath)
		}

		// Track absolute path for later workspace root computation
		absFilePaths = append(absFilePaths, absPath)

		// Check if file is outside workspace and not in /tmp
		isOutsideWorkspace := !isPathInWorkspace(absPath, absWorkspaceDir)
		isInTmp := isPathInTmp(absPath)

		if isOutsideWorkspace && !isInTmp {
			outsidePaths = append(outsidePaths, absPath)
		}

		// Verify the file exists (missing is OK - subagent can create it)
		if _, err := os.Stat(absPath); err != nil && !os.IsNotExist(err) {
			return nil, nil, agenterrors.NewConfig(fmt.Sprintf("failed to access file %s", filePath), err)
		}

		a.Logger().Debug("Validated file path: %s -> %s\n", filePath, absPath)
	}
	return absFilePaths, outsidePaths, nil
}

// approveExternalWorkspace handles the approval flow for external workspace
// access. Returns the computed subagent workspace root (common parent of
// all file paths) or an error if the user rejects.
func approveExternalWorkspace(a *Agent, outsidePaths, absFilePaths []string) (string, error) {
	// Check for auto-approval conditions
	// Unsafe mode bypasses filesystem security checks automatically
	alreadyApproved := a.GetUnsafeMode()
	if !alreadyApproved {
		// Per-folder allowlist: only auto-approve if EVERY outside
		// path is covered by a folder the user previously approved.
		// The old global flag here was the safety bug — approving
		// one path silently allowed all paths for the session.
		alreadyApproved = true
		for _, p := range outsidePaths {
			if !a.IsFolderSessionAllowed(p) {
				alreadyApproved = false
				break
			}
		}
	}

	if !alreadyApproved {
		// CRITICAL: When running as a subagent, we CANNOT prompt for user confirmation
		// because stdin is /dev/null. Instead, we must reject the request.
		if a.IsSubagent() {
			a.Logger().Debug("Subagent encountered external workspace request, cannot prompt for approval (running as subagent)\n")
			return "", agenterrors.NewPermission(fmt.Sprintf("file paths outside workspace require user approval: %v (cannot prompt from subagent context)", outsidePaths), nil)
		}

		// Build approval prompt
		outsidePathsStr := strings.Join(outsidePaths, ", ")
		prompt := fmt.Sprintf("Subagent requests access to files outside the working directory:\n  %s\n\nAllow? This will start the subagent in a directory that covers these files.", outsidePathsStr)

		// Prefer webui approval path when a browser tab is connected
		agentConfig := a.GetConfig()
		logger := utils.GetLogger(agentConfig != nil && agentConfig.SkipPrompt)
		canPrompt := logger != nil && logger.IsInteractive() && !a.IsSubagent()

		if mgr := a.GetSecurityApprovalMgr(); mgr != nil && a.GetEventBus() != nil && !a.IsSubagent() && a.HasActiveWebUIClients() {
			// WEBUI: request approval via event bus for the browser dialog
			extras := map[string]string{
				"risk_type": "Subagent External Workspace",
				"target":    outsidePathsStr,
			}
			if !mgr.RequestToolApproval(a.GetEventBus(), a.GetEventClientID(), a.GetEventUserID(), "run_subagent", "CAUTION", prompt, extras) {
				a.Logger().Debug("User rejected subagent access to external workspace\n")
				return "", agenterrors.NewPermission(fmt.Sprintf("file paths outside workspace rejected by user: %v", outsidePaths), nil)
			}
			a.Logger().Debug("User approved subagent access to external workspace via webui\n")
		} else if canPrompt {
			// CLI: prompt user interactively via terminal stdin
			cliPrompt := "[WARN] Subagent External Workspace\n\n" + prompt + "\n\nAllow? (yes/no): "
			if !logger.AskForConfirmation(cliPrompt, false, false) {
				a.Logger().Debug("User rejected subagent access to external workspace\n")
				return "", agenterrors.NewPermission(fmt.Sprintf("file paths outside workspace rejected by user: %v", outsidePaths), nil)
			}
			a.Logger().Debug("User approved subagent access to external workspace via CLI\n")
		} else {
			// No prompting available (non-interactive): reject
			a.Logger().Debug("Cannot prompt for subagent external workspace approval (non-interactive)\n")
			return "", agenterrors.NewPermission(fmt.Sprintf("file paths outside workspace require approval but prompting is not available: %v", outsidePaths), nil)
		}

		// Mark each outside path's parent as session-allowed so
		// the subagent doesn't re-prompt for the same files.
		// Phase 3 will offer the user a "once vs folder" choice
		// in the dialog itself; for now we widen to parents.
		for _, p := range outsidePaths {
			a.AddSessionAllowedFolder(filepath.Dir(p))
		}
	} else {
		a.Logger().Debug("Auto-approving subagent external workspace (unsafe mode or session bypass)\n")
	}

	// Compute common parent directory of all files as the new workspace root
	subagentWorkspaceRoot := commonParent(absFilePaths)
	a.Logger().Debug("Computed subagent workspace root: %s (from %d file paths)\n", subagentWorkspaceRoot, len(absFilePaths))
	return subagentWorkspaceRoot, nil
}

// overrideWorkspaceRoot replaces the workspace root with workingDir when
// it is explicitly set, and logs a warning if any files fall outside.
func overrideWorkspaceRoot(workspaceRoot, workingDir string, absFilePaths []string, a *Agent) string {
	if workingDir == "" {
		return workspaceRoot
	}
	// Warn if any referenced files fall outside the working_dir scope
	for _, absPath := range absFilePaths {
		if !isPathInWorkspace(absPath, workingDir) && !isPathInTmp(absPath) {
			a.Logger().Debug("Warning: file %s is outside working_dir %s; subagent may not be able to access it\n", absPath, workingDir)
		}
	}
	workspaceRoot = workingDir
	a.Logger().Debug("Overriding subagent workspace root with working_dir: %s\n", workspaceRoot)
	return workspaceRoot
}
