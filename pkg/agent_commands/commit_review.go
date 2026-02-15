package commands

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/alantheprice/ledit/pkg/agent"
	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/factory"
)

// generateCommitReview generates a review of staged files to flag critical concerns
// The review focuses on high-level, critical issues or staged files that should not be committed
func generateCommitReview(chatAgent *agent.Agent) (string, error) {
	// Get staged diff
	diffOutput, err := exec.Command("git", "diff", "--staged").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get staged diff: %v", err)
	}

	diff := strings.TrimSpace(string(diffOutput))
	if len(diff) == 0 {
		return "", nil // No changes to review
	}

	// Get staged files
	stagedFiles, err := getStagedFiles()
	if err != nil {
		return "", fmt.Errorf("failed to get staged files: %v", err)
	}
	if len(stagedFiles) == 0 {
		return "", nil // No staged files to review
	}

	// Use LLM if available to generate review
	var client api.ClientInterface
	if chatAgent != nil {
		configManager := chatAgent.GetConfigManager()
		if configManager != nil {
			if ct, e := configManager.GetProvider(); e == nil {
				model := configManager.GetModelForProvider(ct)
				if cl, ce := factory.CreateProviderClient(ct, model); ce == nil {
					client = cl
				}
			}
		}
	}

	// If no client available, do simple heuristic review
	if client == nil {
		return doHeuristicReview(diff, stagedFiles), nil
	}

	// Use LLM to generate focus on critical concerns
	reviewPrompt := fmt.Sprintf(`You are conducting a code review before a git commit. Focus ONLY on critical concerns.

Staged files (%d):
%s

Diff:
%s

Review the changes and identify ONLY CRITICAL issues that should block this commit:
1. Secrets or credentials (API keys, passwords, tokens, certificates)
2. Security vulnerabilities (SQL injection, command injection, path traversal)
3. Broken functionality (syntax errors, missing imports, broken build)
4. Tests that would fail
5. Temporary/debug code (console.log, fmt.Println, TODOs, commented-out code)
6. Files that should never be committed (large binary files, config files with secrets, .env files)

IMPORTANT RULES:
- If NO critical concerns found, respond with "No critical concerns found."
- If critical concerns ARE found, respond with a clear, concise list (2-3 sentences max)
- ONLY respond with the review text - no preamble, no markdown formatting
- Ignore minor issues like formatting, variable naming, whitespace`, len(stagedFiles), strings.Join(stagedFiles, "\n"), diff)

	messages := []api.Message{
		{
			Role:    "system",
			Content: "You are a code reviewer focusing on critical concerns before a commit.",
		},
		{
			Role:    "user",
			Content: reviewPrompt,
		},
	}

	resp, err := client.SendChatRequest(messages, nil, "")
	if err != nil {
		// Fall back to heuristic review ifLLM fails
		return doHeuristicReview(diff, stagedFiles), nil
	}

	if len(resp.Choices) == 0 {
		return "", nil
	}

	review := strings.TrimSpace(resp.Choices[0].Message.Content)
	return review, nil
}

// doHeuristicReview performs a simple heuristic review when LLM is unavailable
func doHeuristicReview(diff string, stagedFiles []string) string {
	// Check for secrets in diff
	secretPatterns := []string{
		"password", "secret", "api_key", "apikey", "token", "private_key", "private_key",
		"bearer", "authorization", "credential", "passwd", "pwd", "aws_access_key",
		"aws_secret_key", "slack_token", "github_token", "database_url",
	}

	lowerDiff := strings.ToLower(diff)
	for _, pattern := range secretPatterns {
		if strings.Contains(lowerDiff, pattern) {
			return "POTENTIAL SECRET EXPOSED: Changes may contain secrets or credentials. Review carefully before committing."
		}
	}

	// Check for risky file patterns
	for _, file := range stagedFiles {
		lowerFile := strings.ToLower(file)
		if strings.Contains(lowerFile, ".env") ||
			strings.Contains(lowerFile, "secret") ||
			strings.Contains(lowerFile, "credential") ||
			strings.Contains(lowerFile, "private_key") ||
			strings.Contains(lowerFile, ".pem") ||
			strings.Contains(lowerFile, ".key") {
			return fmt.Sprintf("RISKY FILE: %s appears to be a sensitive file that may contain secrets.", file)
		}
	}

	// Check for debug code
	debugPatterns := []string{"console.log", "fmt.println", "print(", "debug=true", "debug=true"}
	for _, pattern := range debugPatterns {
		if strings.Contains(lowerDiff, pattern) {
			return "DEBUG CODE: Changes may contain debug statements or temporary code. Remove before committing."
		}
	}

	// Check for commented code in large chunks
	if strings.Contains(diff, "//") && strings.Contains(diff, "/*") && strings.Count(diff, "//") > 10 {
		return "LARGE COMMENTED CODE BLOCKS: Review to ensure no large blocks of commented code are being committed."
	}

	return "No critical concerns found."
}
