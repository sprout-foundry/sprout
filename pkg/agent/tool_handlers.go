package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Shared utility functions for tool handlers

// isGitCheckoutSubcommand checks if a git command is a checkout or switch operation.
// These are always blocked from shell_command to force use of the git tool
// which requires explicit user approval.
func isGitCheckoutSubcommand(command string) bool {
	trimmed := strings.TrimSpace(command)
	if !strings.HasPrefix(trimmed, "git ") {
		return false
	}
	parts := strings.Fields(trimmed)
	if len(parts) < 2 {
		return false
	}
	// Skip leading flags (e.g., -c key=val, -C path, --no-pager)
	for i := 1; i < len(parts); i++ {
		part := parts[i]
		if strings.HasPrefix(part, "-") {
			// Skip flags that take an argument: -c, -C, --exec-path, etc.
			if part == "-c" || part == "-C" || part == "--exec-path" || part == "--git-dir" || part == "--work-tree" {
				i++ // skip the next argument (the value)
			}
			continue
		}
		sub := strings.TrimPrefix(strings.TrimPrefix(part, "--"), "-")
		return sub == "checkout" || sub == "switch"
	}
	return false
}

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
	writerCommands := []string{
		"commit", "push", "rm", "mv", "reset",
		"rebase", "merge", "checkout", "clean",
		"am", "apply", "cherry-pick", "revert",
		"switch", "restore", "fetch", "pull", "clone",
		"init", "worktree",
	}

	for _, writeCmd := range writerCommands {
		if subcommand == writeCmd {
			return true
		}
	}

	return false
}

// isGitCommitSubcommand checks if a git command is specifically a commit operation
// (as opposed to other write operations like push, merge, etc.)
func isGitCommitSubcommand(command string) bool {
	parts := shellSplit(strings.TrimSpace(command))
	if len(parts) < 2 || parts[0] != "git" {
		return false
	}
	// Skip leading flags and -c key=value config options to find the actual subcommand
	for i := 1; i < len(parts); i++ {
		part := parts[i]
		if part == "-c" {
			// -c takes the next argument as key=value, skip it too
			i++
			continue
		}
		if strings.HasPrefix(part, "-") {
			continue
		}
		subcommand := strings.TrimPrefix(strings.TrimPrefix(part, "--"), "-")
		return subcommand == "commit"
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

// extractGitCommitArgs parses a `git commit ...` command line and extracts
// the message from -m or --message flags. The command comes from an LLM tool
// argument, which may include shell-style quoting (single or double quotes).
// We support basic shell quoting so that `git commit -m "fix: typo"` correctly
// extracts `fix: typo`.
//
// Returns the extracted message (may be empty if no -m/--message flag found).
func extractGitCommitArgs(command string) string {
	tokens := shellSplit(command)
	message := ""

	for i := 0; i < len(tokens)-1; i++ {
		switch tokens[i] {
		case "-m", "--message":
			// Git supports multiple -m flags to build multi-paragraph messages.
			// Each -m becomes a separate paragraph in the commit message.
			if message != "" {
				message += "\n\n"
			}
			message += tokens[i+1]
			i++ // skip the next token (it's the message value)
		}
	}

	return message
}

// shellSplit performs basic shell-style word splitting that respects
// single and double quotes. This is intentionally minimal — it handles
// the common patterns LLMs use when constructing git commit commands.
// It does NOT handle escape sequences, backticks, or variable expansion.
func shellSplit(s string) []string {
	var tokens []string
	var current strings.Builder
	var inQuote rune // 0 = not in quote, '"' or '\'' == in quote
	justClosedQuote := false

	for _, r := range s {
		switch {
		case inQuote != 0:
			if r == inQuote {
				inQuote = 0
				justClosedQuote = true
			} else {
				current.WriteRune(r)
			}
		case r == '"' || r == '\'':
			inQuote = r
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if current.Len() > 0 || justClosedQuote {
				tokens = append(tokens, current.String())
				current.Reset()
			}
			justClosedQuote = false
		default:
			current.WriteRune(r)
			justClosedQuote = false
		}
	}

	if current.Len() > 0 || justClosedQuote {
		tokens = append(tokens, current.String())
	}

	return tokens
}
