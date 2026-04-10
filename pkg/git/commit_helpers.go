package git

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/utils"
)

// CommitExecutor provides methods for executing git commits with message generation.
type CommitExecutor struct {
	Client          api.ClientInterface
	UserMessage     string
	UserInstructions string
	// Dir is the working directory for git commands. If empty, the current directory is used.
	Dir         string
	secretCheck SecretCheckHandler // Optional callback for security checking before commit
}

// SecretCheckHandler is a callback for handling detected secrets in commit flow.
// It receives the security result and returns whether to proceed with the commit.
// Return true to proceed, false to abort.
type SecretCheckHandler func(securityResult CommitSecurityResult) bool

// NewCommitExecutor creates a new commit executor with the given configuration.
func NewCommitExecutor(client api.ClientInterface, userMessage, userInstructions string) *CommitExecutor {
	return &CommitExecutor{
		Client:          client,
		UserMessage:     userMessage,
		UserInstructions: userInstructions,
	}
}

// NewCommitExecutorInDir creates a new commit executor that runs git commands in the specified directory.
func NewCommitExecutorInDir(client api.ClientInterface, userMessage, userInstructions, dir string) *CommitExecutor {
	return &CommitExecutor{
		Client:          client,
		UserMessage:     userMessage,
		UserInstructions: userInstructions,
		Dir:             dir,
	}
}

// NewCommitExecutorWithSecurityCheck creates a CommitExecutor with a security check callback.
func NewCommitExecutorWithSecurityCheck(client api.ClientInterface, userMessage, userInstructions string, secretCheck SecretCheckHandler) *CommitExecutor {
	executor := NewCommitExecutor(client, userMessage, userInstructions)
	executor.secretCheck = secretCheck
	return executor
}

// gitCmd creates an exec.Cmd for a git command in the executor's working directory (if set).
func (e *CommitExecutor) gitCmd(args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...)
	if e.Dir != "" {
		cmd.Dir = e.Dir
	}
	return cmd
}

// ExecuteCommit performs the complete commit operation:
// 1. Gets staged files and validates there are changes
// 2. Gets the current branch name
// 3. Parses file changes into structured format
// 4. Gets the staged diff
// 5. Generates commit message (using provided message, LLM, or fallback)
// 6. Creates commit using a secure temp file
// 7. Returns the commit hash
//
// This function is designed to be reusable by both the commit tool and commit flow.
func (e *CommitExecutor) ExecuteCommit() (string, error) {
	// Get staged files with status
	stagedFilesOutput, err := e.gitCmd("diff", "--cached", "--name-status").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get staged files: %w", err)
	}

	stagedContent := strings.TrimSpace(string(stagedFilesOutput))
	if len(stagedContent) == 0 {
		return "", fmt.Errorf("no staged changes")
	}

	// Get branch name
	var branch string
	branchOutput, err := e.gitCmd("rev-parse", "--abbrev-ref", "HEAD").CombinedOutput()
	if err != nil {
		// On a fresh repo with no commits, HEAD doesn't exist yet.
		// Fall back to the init.defaultBranch or "main".
		branchOutput, err = e.gitCmd("symbolic-ref", "--short", "HEAD").CombinedOutput()
		if err != nil {
			branch = "main"
		} else {
			branch = strings.TrimSpace(string(branchOutput))
		}
	} else {
		branch = strings.TrimSpace(string(branchOutput))
	}

	// Parse file changes into structured format
	fileChanges := parseStagedFileChanges(stagedContent)
	if len(fileChanges) == 0 {
		return "", fmt.Errorf("no staged changes")
	}

	// Get staged diff
	diffOutput, err := e.gitCmd("diff", "--staged").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get diff: %w", err)
	}

	// Run secret detection if a handler is registered
	if e.secretCheck != nil {
		logger := utils.GetLogger(false)
		securityResult := CheckStagedFilesForSecurityCredentials(logger)
		if securityResult.HasConcerns && !e.secretCheck(securityResult) {
			return "", fmt.Errorf("commit aborted: security concerns detected in staged files")
		}
	}

	// Generate commit message
	commitMessage := e.generateCommitMessage(string(diffOutput), branch, fileChanges)
	if commitMessage == "" {
		return "", fmt.Errorf("failed to generate commit message")
	}

	// Create commit using secure temp file
	commitHash, err := e.createCommit(commitMessage)
	if err != nil {
		return "", fmt.Errorf("failed to create commit: %w", err)
	}

	return commitHash, nil
}

// parseStagedFileChanges parses git diff --cached --name-status output into structured file changes.
// Input format: "STATUS\tfilepath" or "STATUS filepath" (tab or space separated)
// Status codes: A (added), D (deleted), M (modified), R (renamed), C (copied), etc.
func parseStagedFileChanges(output string) []CommitFileChange {
	var fileChanges []CommitFileChange
	for _, line := range strings.Split(output, "\n") {
		if line == "" {
			continue
		}
		// Handle both tab and space separated output
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			status := parts[0]
			// Rejoin parts in case filepath contains spaces
			path := strings.Join(parts[1:], " ")
			fileChanges = append(fileChanges, CommitFileChange{
				Status: status,
				Path:   path,
			})
		}
	}
	return fileChanges
}

// generateCommitMessage generates a commit message using the following priority:
// 1. User-provided message (if non-empty)
// 2. User instructions (if non-empty) - used as direct message
// 3. LLM-generated message (if client is available)
// 4. Fallback message based on file changes
func (e *CommitExecutor) generateCommitMessage(diff, branch string, fileChanges []CommitFileChange) string {
	// Priority 1: Use user-provided message directly
	if e.UserMessage != "" {
		return e.UserMessage
	}

	// Priority 2: Use user instructions as direct message
	if e.UserInstructions != "" {
		return e.UserInstructions
	}

	// Priority 3: Generate using LLM
	if e.Client != nil {
		result, err := GenerateCommitMessageFromStagedDiff(e.Client, CommitMessageOptions{
			Diff:        diff,
			Branch:      branch,
			FileChanges: fileChanges,
		})
		if err == nil && result != nil && result.Message != "" {
			return result.Message
		}
	}

	// Priority 4: Fallback message
	return generateFallbackCommitMessage(fileChanges)
}

// generateFallbackCommitMessage creates a simple commit message when LLM is not available.
func generateFallbackCommitMessage(fileChanges []CommitFileChange) string {
	if len(fileChanges) == 0 {
		return "Update files"
	}

	// Categorize changes by type
	var added, deleted, modified []string
	for _, fc := range fileChanges {
		switch fc.Status {
		case "A":
			added = append(added, fc.Path)
		case "D":
			deleted = append(deleted, fc.Path)
		default:
			modified = append(modified, fc.Path)
		}
	}

	totalFiles := len(fileChanges)
	if totalFiles == 1 {
		return fmt.Sprintf("Update %s", fileChanges[0].Path)
	}

	// Build a descriptive message with Title-cased action words
	var parts []string
	if len(added) > 0 {
		parts = append(parts, fmt.Sprintf("Add %d Files", len(added)))
	}
	if len(modified) > 0 {
		parts = append(parts, fmt.Sprintf("Update %d Files", len(modified)))
	}
	if len(deleted) > 0 {
		parts = append(parts, fmt.Sprintf("Delete %d Files", len(deleted)))
	}

	if len(parts) > 0 {
		return strings.Join(parts, ", ")
	}

	return fmt.Sprintf("Update %d files", totalFiles)
}

// createCommit creates a git commit using a secure temporary file for the commit message.
// It automatically cleans up the temporary file after the commit.
func (e *CommitExecutor) createCommit(message string) (string, error) {
	// Create a secure temporary file
	tempFile, err := os.CreateTemp("", "commit_msg_*.txt")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary commit message file: %w", err)
	}
	tempPath := tempFile.Name()
	defer func() {
		if err := os.Remove(tempPath); err != nil && !os.IsNotExist(err) {
			log.Printf("[debug] failed to remove temp commit message file: %v", err)
		}
	}()

	// Write the commit message
	_, err = tempFile.WriteString(message)
	if err != nil {
		tempFile.Close()
		return "", fmt.Errorf("failed to write commit message: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return "", fmt.Errorf("failed to close temporary file: %w", err)
	}

	// Execute git commit
	cmd := e.gitCmd("commit", "-F", tempPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("commit failed: %w\n%s", err, string(output))
	}

	// Get the commit hash
	hashOutput, err := e.gitCmd("rev-parse", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("failed to get commit hash: %w", err)
	}

	return strings.TrimSpace(string(hashOutput)), nil
}