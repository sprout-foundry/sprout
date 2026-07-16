// Package agent: shell working directory and shell command history (split from agent_getters.go)
package agent

import (
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
		// Bare "cd" — goes to $HOME.
	} else if len(trimmed) == 3 && trimmed[2] == ' ' {
		trimmed = trimmed[:2] + strings.TrimSpace(trimmed[3:])
	} else {
		return // e.g., "cddir" — not a cd command.
	}

	// Extract the argument from a compound command (stop at && || ; |).
	var arg string
	for _, sep := range []string{" && ", " || ", ";", " |"} {
		if idx := strings.Index(trimmed, sep); idx >= 0 {
			arg = strings.TrimSpace(trimmed[:idx])
			trimmed = arg
			break
		}
	}
	if arg == "" {
		arg = strings.TrimSpace(trimmed)
	}

	// Read current cwd atomically; fall back to workspace root if unset.
	current, _ := a.ensureShellCwd().GetBoth()
	if current == "" {
		current = a.currentWorkspaceRoot()
	}

	resolved := resolveShellCdArg(arg, current)

	tracker := a.ensureShellCwd()
	if arg == "-" {
		// cd - swaps current and previous.
		tracker.SwapPrevious()
		return
	}

	tracker.SetWithPrev(resolved, current)
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
