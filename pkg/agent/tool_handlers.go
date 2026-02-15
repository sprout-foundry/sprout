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

	// Handle special case: "branch -d" or "branch -D"
	if subcommand == "branch" && len(parts) > 2 {
		// If there's a -d or -D flag, it's a write operation
		for i := 2; i < len(parts); i++ {
			if parts[i] == "-d" || parts[i] == "-D" {
				return true
			}
		}
	}

	// Check if it's a write operation
	writeCommands := []string{
		"commit", "push", "add", "rm", "mv", "reset",
		"rebase", "merge", "checkout", "tag", "clean",
		"stash", "am", "apply", "cherry-pick", "revert",
		"branch", // branch with flags is handled above
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
