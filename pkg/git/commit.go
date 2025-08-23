package git

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/security"
	"github.com/alantheprice/ledit/pkg/utils"
)

// CommitOptions contains options for commit operations
type CommitOptions struct {
	SkipPrompt   bool
	AllowSecrets bool
	Model        string
}

// CheckStagedChanges verifies if there are staged changes
func CheckStagedChanges() error {
	cmd := exec.Command("git", "diff", "--cached", "--quiet", "--exit-code")
	if err := cmd.Run(); err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			// ExitError means there are staged changes (exit code 1)
			return nil
		}
		return fmt.Errorf("failed to check for staged changes: %w", err)
	}
	return fmt.Errorf("no staged changes found")
}

// GetStagedDiff returns the diff of staged changes
func GetStagedDiff() (string, error) {
	cmd := exec.Command("git", "diff", "--cached")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get staged diff: %w", err)
	}
	return string(output), nil
}

// CheckStagedFilesForSecurityCredentials checks staged files for security credentials
func CheckStagedFilesForSecurityCredentials(logger *utils.Logger, cfg *config.Config) bool {
	// Get list of staged files
	cmd := exec.Command("git", "diff", "--cached", "--name-only")
	output, err := cmd.Output()
	if err != nil {
		logger.LogError(fmt.Errorf("failed to get staged files: %w", err))
		return false
	}

	stagedFiles := strings.Split(strings.TrimSpace(string(output)), "\n")
	securityIssuesFound := false

	for _, filePath := range stagedFiles {
		if filePath == "" {
			continue
		}

		// Get the staged diff and analyze only added lines to reduce false positives
		cmd := exec.Command("git", "diff", "--cached", "-U0", "--", filePath)
		diffBytes, err := cmd.Output()
		if err != nil {
			logger.LogError(fmt.Errorf("failed to get staged diff for %s: %w", filePath, err))
			continue
		}

		content := ""
		for _, line := range strings.Split(string(diffBytes), "\n") {
			if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") { // only added lines (ignore diff headers)
				content += strings.TrimPrefix(line, "+") + "\n"
			}
		}

		concerns, _ := security.DetectSecurityConcerns(content)

		if len(concerns) > 0 {
			securityIssuesFound = true
			logger.LogUserInteraction(fmt.Sprintf("Security concerns detected in staged file %s:", filePath))
			for _, concern := range concerns {
				logger.LogUserInteraction(fmt.Sprintf("  - %s", concern))
			}
		}
	}

	return securityIssuesFound
}

// PerformGitCommit executes the git commit command
func PerformGitCommit(message string) error {
	cmd := exec.Command("git", "commit", "-m", message)
	cmd.Stdout = nil // Don't capture stdout
	cmd.Stderr = nil // Don't capture stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git commit failed: %w", err)
	}
	return nil
}

// CleanCommitMessage cleans up LLM-generated commit messages
func CleanCommitMessage(message string) string {
	// Clean up function call format if present
	if strings.Contains(message, `"type": "function"`) && strings.Contains(message, `"name": "generateCommitMessage"`) {
		// Try to parse the JSON and extract meaningful content
		var funcCall map[string]interface{}
		if err := json.Unmarshal([]byte(message), &funcCall); err == nil {
			if params, ok := funcCall["parameters"].(map[string]interface{}); ok {
				// Look for any field that might contain the actual commit message
				if commitMsg, ok := params["commitMessageFormat"].(string); ok && commitMsg != "" {
					return commitMsg
				} else if originalRequest, ok := params["originalUserRequest"].(string); ok && originalRequest != "" {
					// Generate a simple commit message based on the original request
					return fmt.Sprintf("feat: %s\n\n- %s", strings.ToLower(strings.TrimSpace(originalRequest)), "Changes based on user request")
				}
			}
		}
		// Fallback to generic message
		return "feat: add new files and improvements\n\n- Added new test scripts and agent functionality\n- Enhanced code generation and editing capabilities"
	}

	// Clean up the message: remove markdown fences if present
	if strings.HasPrefix(message, "```") && strings.HasSuffix(message, "```") {
		message = strings.TrimPrefix(message, "```")
		message = strings.TrimSuffix(message, "```")
		// Remove language specifier if present (e.g., "git")
		message = strings.TrimPrefix(message, "git\n")
		message = strings.TrimSpace(message)
	}

	return message
}

// ParseCommitMessage parses a commit message into note and description
func ParseCommitMessage(commitMessage string) (string, string, error) {
	lines := strings.Split(commitMessage, "\n")
	if len(lines) < 2 {
		return "", "", fmt.Errorf("failed to parse commit message")
	}

	note := lines[0]
	description := strings.Join(lines[2:], "\n")
	return note, description, nil
}
