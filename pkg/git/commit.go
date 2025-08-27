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

// PerformGitCommit executes the git commit command using HEREDOC format for proper formatting
func PerformGitCommit(message string) error {
	// Use HEREDOC format to ensure proper commit message formatting
	cmdString := fmt.Sprintf(`git commit -m "$(cat <<'EOF'
%s
EOF
)"`, message)

	cmd := exec.Command("bash", "-c", cmdString)
	cmd.Stdout = nil // Don't capture stdout
	cmd.Stderr = nil // Don't capture stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git commit failed: %w", err)
	}
	return nil
}

// CleanCommitMessage cleans up LLM-generated commit messages
func CleanCommitMessage(message string) string {
	// Check if the message looks like JSON (starts and ends with braces)
	trimmed := strings.TrimSpace(message)
	if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
		// Try to parse as JSON
		var jsonObj map[string]interface{}
		if err := json.Unmarshal([]byte(trimmed), &jsonObj); err == nil {

			// Handle function call format
			if strings.Contains(message, `"type": "function"`) && strings.Contains(message, `"name": "generateCommitMessage"`) {
				if params, ok := jsonObj["parameters"].(map[string]interface{}); ok {
					if commitMsg, ok := params["commitMessageFormat"].(string); ok && commitMsg != "" {
						return commitMsg
					} else if originalRequest, ok := params["originalUserRequest"].(string); ok && originalRequest != "" {
						return fmt.Sprintf("🚀 feat: %s\n\n- %s", strings.ToLower(strings.TrimSpace(originalRequest)), "Changes based on user request")
					}
				}
			}

			// Handle simple key-value JSON format like {"title": "description"}
			if len(jsonObj) == 1 {
				for title, desc := range jsonObj {
					if descStr, ok := desc.(string); ok && title != "" && descStr != "" {
						// Determine emoji based on title content
						emoji := "🚀"
						prefix := "feat"
						titleLower := strings.ToLower(title)
						if strings.Contains(titleLower, "fix") || strings.Contains(titleLower, "bug") {
							emoji = "🐛"
							prefix = "fix"
						} else if strings.Contains(titleLower, "doc") {
							emoji = "📝"
							prefix = "docs"
						} else if strings.Contains(titleLower, "enhance") || strings.Contains(titleLower, "improve") {
							emoji = "✨"
							prefix = "enhance"
						} else if strings.Contains(titleLower, "refactor") {
							emoji = "♻️"
							prefix = "refactor"
						} else if strings.Contains(titleLower, "test") {
							emoji = "🧪"
							prefix = "test"
						}

						return fmt.Sprintf("%s %s: %s\n\n%s", emoji, prefix, title, descStr)
					}
				}
			}
		}

		// Fallback for JSON that couldn't be parsed properly
		return "🚀 feat: Add new functionality\n\n- Enhanced codebase with new features\n- Improved system capabilities"
	}

	// Clean up the message: remove markdown fences if present
	if strings.HasPrefix(message, "```") && strings.HasSuffix(message, "```") {
		message = strings.TrimPrefix(message, "```")
		message = strings.TrimSuffix(message, "```")
		// Remove language specifier if present (e.g., "git")
		message = strings.TrimPrefix(message, "git\n")
		message = strings.TrimSpace(message)
	}

	// Normalize commit message format: ensure exactly one blank line between title and description
	lines := strings.Split(message, "\n")
	if len(lines) > 2 {
		// Find the first non-empty line after the title (index 0)
		titleLine := strings.TrimSpace(lines[0])
		var descriptionStart int = -1

		// Find where the description starts (first non-empty line after title)
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) != "" {
				descriptionStart = i
				break
			}
		}

		if descriptionStart > 1 {
			// There are multiple empty lines between title and description
			// Reconstruct with exactly one blank line
			description := strings.Join(lines[descriptionStart:], "\n")
			message = titleLine + "\n\n" + description
		}
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
