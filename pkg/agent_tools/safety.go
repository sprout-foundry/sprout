package tools

import (
	"regexp"
	"strings"
)

// DestructiveCommand represents a potentially destructive command
type DestructiveCommand struct {
	Pattern     string
	Description string
	RiskLevel   string // "high", "medium", "low"
}

// DestructiveCommands is a list of patterns that match potentially destructive commands
var DestructiveCommands = []DestructiveCommand{
	// High risk - irreversible data loss
	{Pattern: `^\s*rm\s+-rf?\s+`, Description: "Recursive file deletion", RiskLevel: "high"},
	{Pattern: `^\s*rm\s+-fr\s+`, Description: "Recursive file deletion", RiskLevel: "high"},
	{Pattern: `^\s*rm\s+.*\*`, Description: "Wildcard file deletion", RiskLevel: "high"},
	{Pattern: `^\s*rmdir\s+.*/`, Description: "Directory deletion", RiskLevel: "high"},
	{Pattern: `^\s*dd\s+`, Description: "Disk/device manipulation", RiskLevel: "high"},
	{Pattern: `^\s*mv\s+.*\s+/dev/null`, Description: "Redirect to /dev/null", RiskLevel: "high"},
	{Pattern: `^\s*>\s+.*`, Description: "File truncation/overwrite", RiskLevel: "high"},

	// Medium risk - data modification
	{Pattern: `^\s*git\s+checkout\s+`, Description: "Git checkout (potential data loss)", RiskLevel: "medium"},
	{Pattern: `^\s*git\s+reset\s+--hard`, Description: "Hard git reset", RiskLevel: "medium"},
	{Pattern: `^\s*git\s+clean\s+-fd`, Description: "Git clean with force", RiskLevel: "medium"},
	{Pattern: `^\s*chmod\s+[0-7]{3,4}\s+`, Description: "File permission changes", RiskLevel: "medium"},
	{Pattern: `^\s*chown\s+`, Description: "File ownership changes", RiskLevel: "medium"},

	// Low risk - system operations
	{Pattern: `^\s*kill\s+`, Description: "Process termination", RiskLevel: "low"},
	{Pattern: `^\s*pkill\s+`, Description: "Process termination by name", RiskLevel: "low"},
	{Pattern: `^\s*reboot\s+`, Description: "System reboot", RiskLevel: "low"},
	{Pattern: `^\s*shutdown\s+`, Description: "System shutdown", RiskLevel: "low"},
}

// IsDestructiveCommand checks if a command is potentially destructive
func IsDestructiveCommand(command string) (*DestructiveCommand, bool) {
	command = strings.TrimSpace(command)

	for _, destructiveCmd := range DestructiveCommands {
		regex := regexp.MustCompile(destructiveCmd.Pattern)
		if regex.MatchString(command) {
			return &destructiveCmd, true
		}
	}

	return nil, false
}

// IsFileDeletionCommand checks if a command will delete files
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

// GetCommandRiskLevel returns the risk level of a command
func GetCommandRiskLevel(command string) string {
	if destructiveCmd, isDestructive := IsDestructiveCommand(command); isDestructive {
		return destructiveCmd.RiskLevel
	}
	return "none"
}
