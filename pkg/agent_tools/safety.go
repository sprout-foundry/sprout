package tools

import (
	"strings"
)

// IsFileDeletionCommand checks if a command will delete files
// This is used for change tracking (not security validation)
// Security validation is handled by the LLM-based validator in tool_registry.go
func IsFileDeletionCommand(command string) bool {
	command = strings.TrimSpace(strings.ToLower(command))

	// Check for rm commands
	if strings.HasPrefix(command, "rm ") {
		return true
	}

	// Check for git clean with force
	if strings.Contains(command, "git clean") && strings.Contains(command, "-f") {
		return true
	}

	// Check for rmdir
	if strings.HasPrefix(command, "rmdir ") {
		return true
	}

	return false
}
