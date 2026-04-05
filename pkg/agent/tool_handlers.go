package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Shared utility functions for tool handlers

// isGitWriteCommand checks if a command is a git write operation (which should use git tool for approval)
func isGitWriteCommand(command string) bool {
	trimmed := strings.TrimSpace(command)
	if !strings.HasPrefix(trimmed, "git ") {
		return false
	}

	// Extract the git subcommand (e.g., "git log" -> "log")
	// Handle git -c flag and other options before subcommand
	parts := strings.Fields(trimmed)
	if len(parts) < 2 {
		return false // Not a complete git command
	}

	// Find the actual subcommand (skip "git" and any leading flags like -c, -C, etc.)
	subcommand := ""
	for i := 1; i < len(parts); i++ {
		part := parts[i]
		// Skip common git options that appear before subcommand
		if strings.HasPrefix(part, "-") {
			continue
		}
		subcommand = part
		break
	}

	if subcommand == "" {
		return false
	}

	// Normalize subcommand (remove dashes, handle branch -d/-D as "branch")
	subcommand = strings.TrimPrefix(subcommand, "--")
	subcommand = strings.TrimPrefix(subcommand, "-")

	// Handle special subcommands that can be read or write depending on flags/args.
	rest := parts[2:]
	switch subcommand {
	case "branch":
		// Read-only examples: git branch, git branch -a, git branch --list
		// Write examples: git branch new-feature, git branch -d old-feature
		branchWriteFlags := map[string]struct{}{
			"-d": {}, "-D": {}, "--delete": {}, "-m": {}, "-M": {}, "--move": {},
			"-c": {}, "-C": {}, "--copy": {}, "-f": {}, "--force": {},
			"-u": {}, "--set-upstream-to": {}, "--unset-upstream": {}, "--edit-description": {},
		}
		for _, arg := range rest {
			if _, ok := branchWriteFlags[arg]; ok {
				return true
			}
			// A positional argument (that isn't a list flag) generally means create/update branch.
			if !strings.HasPrefix(arg, "-") {
				return true
			}
		}
		return false
	case "tag":
		// Read-only examples: git tag, git tag -l
		// Write examples: git tag v1.2.3, git tag -d v1.2.3
		tagWriteFlags := map[string]struct{}{
			"-d": {}, "--delete": {}, "-a": {}, "-s": {}, "-u": {}, "-f": {}, "--force": {},
		}
		for _, arg := range rest {
			if _, ok := tagWriteFlags[arg]; ok {
				return true
			}
			if !strings.HasPrefix(arg, "-") {
				return true
			}
		}
		return false
	case "stash":
		// Read-only: git stash list/show
		// Write: git stash [push|pop|apply|drop|clear|branch|store]
		if len(rest) == 0 {
			return true // plain `git stash` is equivalent to push
		}
		action := rest[0]
		switch action {
		case "list", "show":
			return false
		default:
			return true
		}
	}

	// Staging operations (git add) are always allowed per policy — not considered a restricted write.
	if subcommand == "add" {
		return false
	}

	// Check if it's a write operation
	writeCommands := []string{
		"commit", "push", "rm", "mv", "reset",
		"rebase", "merge", "checkout", "clean",
		"am", "apply", "cherry-pick", "revert",
		"switch", "restore", "fetch", "pull", "clone",
		"init", "worktree",
	}

	for _, writeCmd := range writeCommands {
		if subcommand == writeCmd {
			return true
		}
	}

	return false
}

// convertToString safely converts a parameter to string with proper error handling
func convertToString(param interface{}, paramName string) (string, error) {
	switch v := param.(type) {
	case string:
		return v, nil
	case []byte:
		return string(v), nil
	case int, int32, int64, float32, float64:
		return fmt.Sprintf("%v", v), nil
	case bool:
		return fmt.Sprintf("%t", v), nil
	case map[string]interface{}:
		// If it's a map, try to convert to JSON string
		jsonBytes, err := json.Marshal(v)
		if err != nil {
			return "", fmt.Errorf("parameter '%s' is an object that cannot be converted to string: %w", paramName, err)
		}
		return string(jsonBytes), nil
	case nil:
		return "", fmt.Errorf("parameter '%s' is missing or null", paramName)
	default:
		return "", fmt.Errorf("parameter '%s' has invalid type %T, expected string", paramName, param)
	}
}
