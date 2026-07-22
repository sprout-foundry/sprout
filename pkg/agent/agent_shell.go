// Package agent: shell working directory and shell command history (split from agent_getters.go)
package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GetShellCwd returns the current logical shell working directory.
func (a *Agent) GetShellCwd() string {
	return a.ensureShellCwd().Get()
}

// SetShellCwd sets the logical shell working directory and records the previous.
func (a *Agent) SetShellCwd(dir string) {
	a.ensureShellCwd().Set(dir)
}

// effectiveCwd returns the directory that tools should use for file/git operations.
// It returns shellCwd when set (updated by cd commands), falling back to the workspace root.
func (a *Agent) effectiveCwd() string {
	if cwd := a.GetShellCwd(); cwd != "" {
		return cwd
	}
	return a.currentWorkspaceRoot()
}

// updateShellCwd parses a shell command string and updates the tracked
// shell working directory when the command is a cd directive.
// It handles: cd <path>, cd, cd -, cd ~, cd .., cd <path> &&/;/|| <more>.
// It does NOT update for subshell cd (e.g., "(cd /path && ...)").
//
// cd targets are validated against the agent's workspace root and
// session-allowlisted folders. Rejected targets leave the tracked
// cwd unchanged and emit a user-visible rejection message.
func (a *Agent) updateShellCwd(cmd string) {
	trimmed := strings.TrimSpace(cmd)

	// Skip subshells — they don't affect the parent shell's CWD.
	if strings.HasPrefix(trimmed, "(") {
		return
	}

	// Only act on commands that start with "cd"
	if !strings.HasPrefix(trimmed, "cd") {
		return
	}

	// Must be "cd" alone or "cd " followed by arguments.
	if len(trimmed) == 2 {
		// Bare "cd" — arg will be set to empty below.
	} else if len(trimmed) >= 3 && trimmed[2] == ' ' {
		// Has "cd " prefix with a path — strip the "cd " prefix for extraction.
		trimmed = strings.TrimSpace(trimmed[3:])
	} else {
		return // e.g., "cddir" — not a cd command.
	}

	// Extract the argument from a compound command (stop at && || ; |).
	// After stripping "cd " prefix, trimmed contains just the path (or "" for bare cd).
	var arg string
	extracted := false // tracks whether we extracted arg from a compound command
	for _, sep := range []string{" && ", " || ", ";", " |"} {
		if idx := strings.Index(trimmed, sep); idx >= 0 {
			extractedArg := strings.TrimSpace(trimmed[:idx])
			// Strip leading "cd " if present (for compound commands like "cd /path && ...").
			if strings.HasPrefix(extractedArg, "cd ") {
				arg = strings.TrimSpace(extractedArg[3:])
			} else if extractedArg == "cd" {
				arg = "" // bare "cd" in compound
			} else {
				arg = extractedArg
			}
			extracted = true
			break
		}
	}
	// If no compound separator, use the full trimmed (the path).
	// For bare "cd", trimmed is just "cd" and arg should be empty.
	if !extracted {
		if trimmed != "cd" {
			arg = trimmed
		} else {
			arg = ""
		}
	}

	// Read current and previous cwd atomically; fall back to workspace root if unset.
	current, prev := a.ensureShellCwd().GetBoth()
	if current == "" {
		current = a.currentWorkspaceRoot()
	}

	tracker := a.ensureShellCwd()

	if arg == "-" {
		// cd - swaps current and previous.
		// Validate the destination (previous) is allowed before swapping.
		if prev == "" {
			// No previous directory to switch to.
			return
		}
		// Validate the destination (previous) is allowed before swapping.
		if !a.IsCdTargetAllowed(prev) {
			a.writeCdRejectionMessage(prev, "previous directory is not allowed")
			return
		}
		tracker.SwapPrevious()
		return
	}

	resolved := resolveShellCdArg(arg, current)

	// Gate: validate the resolved target against the allowlist.
	if !a.IsCdTargetAllowed(resolved) {
		a.writeCdRejectionMessage(resolved, "is not in the workspace or the workflow's declared allowed_paths")
		return
	}

	tracker.SetWithPrev(resolved, current)
}

// writeCdRejectionMessage writes a user-visible rejection message when a
// cd target is not allowed. The message includes the rejected target and
// lists currently allowed cd targets for reference.
func (a *Agent) writeCdRejectionMessage(target, reason string) {
	allowed := a.ListAllowedCdTargets()

	// Format the allowed targets for the message.
	var allowedList string
	if len(allowed) == 0 {
		allowedList = "(no allowed paths defined)"
	} else {
		for i, path := range allowed {
			if i > 0 {
				allowedList += ", "
			}
			allowedList += path
		}
	}

	message := "cd refused: " + target + " " + reason + ". Currently allowed: " + allowedList + ".\n"

	// Write to debug log.
	a.debugLog("CD_REFUSED: %s", message)

	// Write to stderr for CLI visibility.
	fmt.Fprintf(os.Stderr, "%s", message)

	// Write to terminal writer if available (WebUI mode).
	if a.output != nil {
		if tw := a.output.GetTerminalWriter(); tw != nil {
			tw(message)
		}
	}
}

// resolveShellCdArg resolves a cd argument to an absolute path.
func resolveShellCdArg(arg, currentCwd string) string {
	if arg == "" {
		// Bare cd → $HOME.
		if home := os.Getenv("HOME"); home != "" {
			return home
		}
		return currentCwd
	}
	if arg == "-" {
		return "-" // Handled specially by caller.
	}
	if arg == "~" {
		if home := os.Getenv("HOME"); home != "" {
			return home
		}
		return currentCwd
	}
	if strings.HasPrefix(arg, "~/") {
		if home := os.Getenv("HOME"); home != "" {
			return filepath.Join(home, arg[2:])
		}
		return arg[2:]
	}
	if !filepath.IsAbs(arg) {
		return filepath.Join(currentCwd, arg)
	}
	return arg
}

// GetShellCommandHistoryEntry retrieves a shell command result from history
func (a *Agent) GetShellCommandHistoryEntry(command string) (*ShellCommandResult, bool) {
	a.shellCommandHistoryMu.RLock()
	defer a.shellCommandHistoryMu.RUnlock()
	result, exists := a.shellCommandHistory[command]
	return result, exists
}

// SetShellCommandHistoryEntry stores a shell command result in history
func (a *Agent) SetShellCommandHistoryEntry(command string, result *ShellCommandResult) {
	a.shellCommandHistoryMu.Lock()
	defer a.shellCommandHistoryMu.Unlock()
	a.shellCommandHistory[command] = result
}

// ClearShellCommandHistory removes all entries from shell command history
func (a *Agent) ClearShellCommandHistory() {
	a.shellCommandHistoryMu.Lock()
	defer a.shellCommandHistoryMu.Unlock()
	a.shellCommandHistory = make(map[string]*ShellCommandResult)
}

// GetAllShellCommandHistory returns a copy of the shell command history
func (a *Agent) GetAllShellCommandHistory() map[string]*ShellCommandResult {
	a.shellCommandHistoryMu.RLock()
	defer a.shellCommandHistoryMu.RUnlock()
	result := make(map[string]*ShellCommandResult, len(a.shellCommandHistory))
	for k, v := range a.shellCommandHistory {
		result[k] = v
	}
	return result
}
