package configuration

import (
	"strings"
)

// RiskLevel represents the risk classification of an operation for the EA approval cascade.
type RiskLevel string

const (
	RiskLevelLow    RiskLevel = "low"    // Auto-approve (git status, read operations)
	RiskLevelMedium RiskLevel = "medium" // Reason and decide (git commit, git push)
	RiskLevelHigh   RiskLevel = "high"   // Always reject (force flags, rm -rf)
)

// AutoApproveRules controls the EA's sliding risk cascade for operation approvals.
type AutoApproveRules struct {
	LowRiskOps     []string `json:"low_risk,omitempty"`      // Operations auto-approved by EA
	MediumRiskOps  []string `json:"medium_risk,omitempty"`   // Operations the EA reasons about
	HighRiskNever  []string `json:"high_risk_never,omitempty"` // Operations always rejected
}

// DefaultAutoApproveRules returns the default risk cascade rules for the EA persona.
func DefaultAutoApproveRules() AutoApproveRules {
	return AutoApproveRules{
		LowRiskOps: []string{
			"git_add", "git_status", "git_log", "git_diff",
			"read_file",
		},
		MediumRiskOps: []string{
			"git_commit", "git_push", "git_pull", "git_fetch",
			"write_file", "edit_file", "shell_command",
			"rm_command", "docker",
			"subagent_spawn", "cross_directory",
		},
		HighRiskNever: []string{
			"force_flag", "rm_recursive", "git_reset_hard",
			"git_clean", "docker_prune", "git_push_force",
			"git_checkout", "git_switch", "git_restore", "git_branch_delete",
		},
	}
}

// GetAutoApproveRules returns the auto-approve rules for this persona,
// falling back to defaults if none are configured.
// Callers MUST NOT modify the returned struct's slice fields,
// as they may share backing arrays with the original config.
func (st *SubagentType) GetAutoApproveRules() AutoApproveRules {
	if st.AutoApproveRules != nil {
		return *st.AutoApproveRules
	}
	return DefaultAutoApproveRules()
}

// EvaluateOperationRisk determines the risk level of a shell operation
// based on the persona's auto-approve rules.
// Returns RiskLevelLow, RiskLevelMedium, or RiskLevelHigh.
func (st *SubagentType) EvaluateOperationRisk(command string) RiskLevel {
	rules := st.GetAutoApproveRules()

	cmdLower := strings.ToLower(command)

	// Always check for force flags first — -f/--force always escalates to high risk
	if containsForceFlag(cmdLower) {
		return RiskLevelHigh
	}

	// Check high-risk patterns
	for _, pattern := range rules.HighRiskNever {
		if matchesRiskPattern(cmdLower, pattern) {
			return RiskLevelHigh
		}
	}

	// Determine the operation category for classification
	opCategory := categorizeCommand(cmdLower)

	// Check if the operation is explicitly in the low-risk list
	for _, pattern := range rules.LowRiskOps {
		if opCategory == pattern {
			return RiskLevelLow
		}
	}

	// Check if the operation is in the medium-risk list
	for _, pattern := range rules.MediumRiskOps {
		if opCategory == pattern {
			return RiskLevelMedium
		}
	}

	// Default to medium for unrecognized operations — the EA reasons about them
	return RiskLevelMedium
}

// containsForceFlag checks if a command string contains -f or --force flags.
// --force-with-lease is explicitly excluded as it is a safer alternative
// that verifies remote state before overwriting.
// For -f standalone, only treats it as force for commands that commonly use -f as a force flag:
// git, rm, mv, cp, and docker. This avoids false positives on commands like grep -f or tail -f.
func containsForceFlag(cmdLower string) bool {
	// Check for --force as an exact token, but NOT --force-with-lease
	for _, segment := range strings.Fields(cmdLower) {
		if segment == "--force" {
			return true
		}
	}

	// Get the first word (command name) to check if -f should be treated as force
	fields := strings.Fields(cmdLower)
	if len(fields) == 0 {
		return false
	}
	firstCmd := fields[0]

	// Check for -f as a standalone flag (not part of a word)
	// Only treat -f as force for commands that commonly use it as a force flag
	for _, segment := range strings.Fields(cmdLower) {
		if segment == "-f" {
			// Only -f for force-capable commands
			switch firstCmd {
			case "git", "rm", "mv", "cp", "docker":
				return true
			}
		}
		// Handle combined short flags like -af, -rf (these are dangerous)
		// Only treat combined flags with 'f' as force for force-capable commands
		if len(segment) > 2 && segment[0] == '-' && segment[1] != '-' && strings.Contains(segment, "f") {
			switch firstCmd {
			case "git", "rm", "mv", "cp", "docker":
				// But not things like "diff" or "conf" that happen to contain f
				// Only flag combinations that include f
				isAllFlags := true
				for _, ch := range segment[1:] {
					if ch >= '0' && ch <= '9' {
						isAllFlags = false
						break
					}
				}
				if isAllFlags {
					return true
				}
			}
		}
	}
	return false
}

// categorizeCommand maps a command string to a risk-category identifier.
func categorizeCommand(cmdLower string) string {
	if strings.HasPrefix(cmdLower, "git ") {
		return categorizeGitCommand(cmdLower)
	}
	if strings.HasPrefix(cmdLower, "rm ") {
		return "rm_command"
	}
	if strings.HasPrefix(cmdLower, "docker ") {
		return "docker"
	}
	// Read-only file operations
	if strings.HasPrefix(cmdLower, "cat ") || strings.HasPrefix(cmdLower, "head ") ||
		strings.HasPrefix(cmdLower, "ls ") || strings.HasPrefix(cmdLower, "find ") ||
		strings.HasPrefix(cmdLower, "which ") || strings.HasPrefix(cmdLower, "file ") {
		return "read_file"
	}
	// Write operations
	if strings.HasPrefix(cmdLower, "write_file") || strings.HasPrefix(cmdLower, "edit_file") {
		return "write_file"
	}
	return "shell_command"
}

// categorizeGitCommand maps git subcommands to risk-category identifiers.
func categorizeGitCommand(cmdLower string) string {
	subcmd := firstFieldAfter(cmdLower, "git")
	switch subcmd {
	case "status":
		return "git_status"
	case "log":
		return "git_log"
	case "diff":
		return "git_diff"
	case "add":
		return "git_add"
	case "commit":
		return "git_commit"
	case "push":
		return "git_push"
	case "pull":
		return "git_pull"
	case "fetch":
		return "git_fetch"
	case "reset":
		return "git_reset_hard"
	case "clean":
		return "git_clean"
	case "branch":
		if strings.Contains(cmdLower, "-d") || strings.Contains(cmdLower, "--delete") {
			return "git_branch_delete" // Branch deletion is high risk
		}
		return "git_status" // Branch listing is low risk
	case "checkout":
		return "git_checkout" // Can discard changes
	case "switch":
		return "git_switch" // Can discard changes
	case "restore":
		return "git_restore" // Can discard changes
	case "stash":
		return "git_status" // Stash is relatively safe
	case "tag":
		return "git_add" // Tags are relatively safe
	case "merge", "rebase":
		return "git_commit" // Medium risk like commit
	default:
		return "shell_command" // Default to medium
	}
}

// matchesRiskPattern checks if a command matches a risk pattern identifier.
func matchesRiskPattern(cmdLower string, pattern string) bool {
	// Map pattern names to actual command matching
	switch pattern {
	case "force_flag":
		return containsForceFlag(cmdLower)
	case "rm_recursive":
		return strings.Contains(cmdLower, "rm ") && (strings.Contains(cmdLower, "-r") || strings.Contains(cmdLower, "-rf") || strings.Contains(cmdLower, "--recursive"))
	case "git_reset_hard":
		return strings.Contains(cmdLower, "git reset") && strings.Contains(cmdLower, "--hard")
	case "git_clean":
		return strings.Contains(cmdLower, "git clean")
	case "git_push_force":
		if !strings.Contains(cmdLower, "git push") {
			return false
		}
		// --force-with-lease is safer, don't match it
		for _, segment := range strings.Fields(cmdLower) {
			if segment == "--force" || segment == "-f" {
				return true
			}
		}
		return false
	case "docker_prune":
		return strings.Contains(cmdLower, "docker") && strings.Contains(cmdLower, "prune")
	case "git_checkout":
		return strings.Contains(cmdLower, "git checkout")
	case "git_switch":
		return strings.Contains(cmdLower, "git switch")
	case "git_restore":
		return strings.Contains(cmdLower, "git restore")
	case "git_branch_delete":
		return strings.Contains(cmdLower, "git branch") && (strings.Contains(cmdLower, "-d") || strings.Contains(cmdLower, "-D") || strings.Contains(cmdLower, "--delete"))
	default:
		return false
	}
}

// firstFieldAfter returns the first whitespace-delimited field after the given prefix.
func firstFieldAfter(s, prefix string) string {
	after := strings.TrimPrefix(s, prefix)
	after = strings.TrimSpace(after)
	fields := strings.Fields(after)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}