package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/codereview"
	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/factory"
	"github.com/alantheprice/ledit/pkg/utils"

	"github.com/spf13/cobra"
)

var (
	reviewStagedModel      string
	reviewStagedSkipPrompt bool // Not strictly necessary for review, but consistent with other commands
)

var reviewStagedCmd = &cobra.Command{
	Use:   "review",
	Short: "Perform an AI-powered code review on staged Git changes",
	Long: `This command uses an LLM to review your currently staged Git changes.
It provides feedback on code quality, potential issues, and suggestions for improvement.`,
	Run: func(cmd *cobra.Command, args []string) {
		logger := utils.GetLogger(reviewStagedSkipPrompt)

		cfg, err := configuration.LoadOrInitConfig(reviewStagedSkipPrompt)
		if err != nil {
			logger.LogError(fmt.Errorf("failed to load or initialize config: %w", err))
			return
		}

		// Override model if specified by flag
		var customAgentClient api.ClientInterface
		if reviewStagedModel != "" {
			// Parse provider from model string (e.g., "ollama:llama3" or "openai:gpt-4")
			clientType, err := api.DetermineProvider(reviewStagedModel, api.ClientType(cfg.LastUsedProvider))
			if err != nil {
				logger.LogError(fmt.Errorf("failed to determine provider from model '%s': %w", reviewStagedModel, err))
				return
			}
			customAgentClient, err = factory.CreateProviderClient(clientType, reviewStagedModel)
			if err != nil {
				logger.LogError(fmt.Errorf("failed to create agent client with model '%s': %w", reviewStagedModel, err))
				return
			}
			logger.LogProcessStep(fmt.Sprintf("Using custom model: %s", reviewStagedModel))
		}

		// Check for staged changes
		cmdCheckStaged := exec.Command("git", "diff", "--cached", "--quiet", "--exit-code")
		if err := cmdCheckStaged.Run(); err != nil {
			// If err is not nil, it means there are staged changes (exit code 1) or another error
			if _, ok := err.(*exec.ExitError); ok {
				// ExitError means git exited with a non-zero status, which is what we want for staged changes
				logger.LogProcessStep("Staged changes detected. Performing code review...")
			} else {
				logger.LogError(fmt.Errorf("failed to check for staged changes: %w", err))
				return
			}
		} else {
			logger.LogUserInteraction("No staged changes found. Please stage your changes before running 'ledit review'.")
			return
		}

		// Get the diff of staged changes
		cmdDiff := exec.Command("git", "diff", "--cached")
		stagedDiffBytes, err := cmdDiff.Output()
		if err != nil {
			logger.LogError(fmt.Errorf("failed to get staged diff: %w", err))
			return
		}
		stagedDiff := string(stagedDiffBytes)

		if strings.TrimSpace(stagedDiff) == "" {
			logger.LogUserInteraction("No actual diff content found in staged changes. Nothing to review.")
			return
		}

		// Optimize diff for code review (uses higher thresholds and better filtering)
		optimizer := utils.NewDiffOptimizerForReview()
		optimizedDiff := optimizer.OptimizeDiff(stagedDiff)

		// Create the review context with optimized diff
		reviewDiff := optimizedDiff.OptimizedContent

		// Add file summaries to context if available
		if len(optimizedDiff.FileSummaries) > 0 {
			var summaryInfo strings.Builder
			summaryInfo.WriteString("\n\nThe following files were optimized (only summaries shown):\n")
			for file, summary := range optimizedDiff.FileSummaries {
				summaryInfo.WriteString(fmt.Sprintf("- %s: %s\n", file, summary))
			}
			reviewDiff += summaryInfo.String()
		}

		// Extract metadata for enhanced review context
		// These help the LLM understand intent and avoid false positives
		projectType := detectProjectType()
		commitMessage := extractStagedChangesSummary()
		keyComments := extractKeyCommentsFromDiff(stagedDiff)
		changeCategories := categorizeChanges(stagedDiff)

		// Create the unified code review service
		service := codereview.NewCodeReviewService(cfg, logger)

		// Use custom agent client if model flag was set, otherwise use default
		agentClient := customAgentClient
		if agentClient == nil {
			agentClient = service.GetDefaultAgentClient()
		}

		// This helps avoid false positives when functionality moved across files.
		fullFileContext := extractFileContextForChanges(stagedDiff)

		// Create the review context with metadata
		ctx := &codereview.ReviewContext{
			Diff:             reviewDiff,
			Config:           cfg,
			Logger:           logger,
			AgentClient:      agentClient,
			ProjectType:      projectType,
			CommitMessage:    commitMessage,
			KeyComments:      keyComments,
			ChangeCategories: changeCategories,
			FullFileContext:  fullFileContext,
		}

		// Create review options for staged review
		opts := &codereview.ReviewOptions{
			Type:             codereview.StagedReview,
			SkipPrompt:       reviewStagedSkipPrompt,
			RollbackOnReject: false, // Don't rollback for staged reviews
		}

		reviewResponse, err := service.PerformReview(ctx, opts)
		if err != nil {
			logger.LogError(fmt.Errorf("failed to get code review from LLM: %w", err))
			return
		}

		logger.LogUserInteraction("\n--- AI Code Review ---")
		logger.LogUserInteraction(fmt.Sprintf("Status: %s", strings.ToUpper(reviewResponse.Status)))
		logger.LogUserInteraction(fmt.Sprintf("Feedback:\n%s", reviewResponse.Feedback))

		// If review needs revision and not in skip-prompt mode, offer agentic review
		if !reviewStagedSkipPrompt && (reviewResponse.Status == "needs_revision" || reviewResponse.Status == "rejected") {
			logger.LogUserInteraction("\n")
			prompt := "The review identified issues that need attention. Would you like to run a deeper agentic review (with file reading tools) for more accurate analysis? This may take longer but can provide better context. (yes/no): "

			if logger.AskForConfirmation(prompt, false, false) {
				agenticResponse, err := service.PerformAgenticReview(ctx, opts)
				if err != nil {
					logger.LogUserInteraction("Note: Agentic review mode is not yet implemented. Using initial review results.")
				} else {
					logger.LogUserInteraction("\n--- Agentic Review Results ---")
					logger.LogUserInteraction(fmt.Sprintf("Status: %s", strings.ToUpper(agenticResponse.Status)))
					logger.LogUserInteraction(fmt.Sprintf("Feedback:\n%s", agenticResponse.Feedback))

					if agenticResponse.NewPrompt != "" {
						logger.LogUserInteraction(fmt.Sprintf("\nSuggested New Prompt:\n%s", agenticResponse.NewPrompt))
					}
				}
			}
		}

		if reviewResponse.Status == "rejected" && reviewResponse.NewPrompt != "" {
			logger.LogUserInteraction(fmt.Sprintf("\nSuggested New Prompt for Re-execution:\n%s", reviewResponse.NewPrompt))
		}
		logger.LogUserInteraction("----------------------")
	},
}

func init() {
	reviewStagedCmd.Flags().StringVarP(&reviewStagedModel, "model", "m", "", "Specify the LLM model to use for the code review (e.g., 'ollama:llama3')")
	reviewStagedCmd.Flags().BoolVar(&reviewStagedSkipPrompt, "skip-prompt", false, "Skip any interactive prompts (e.g., for confirmation, though less relevant for review)")
}

// detectProjectType detects the type of project based on files in the current directory
func detectProjectType() string {
	// Check for Go project
	if _, err := os.Stat("go.mod"); err == nil {
		return "Go project"
	}

	// Check for Node.js project (package.json)
	if _, err := os.Stat("package.json"); err == nil {
		return "Node.js project"
	}

	// Check for Python project (requirements.txt, setup.py, pyproject.toml)
	if _, err := os.Stat("requirements.txt"); err == nil {
		return "Python project"
	}
	if _, err := os.Stat("setup.py"); err == nil {
		return "Python project"
	}
	if _, err := os.Stat("pyproject.toml"); err == nil {
		return "Python project"
	}

	// Check for Rust project
	if _, err := os.Stat("Cargo.toml"); err == nil {
		return "Rust project"
	}

	// Check for Ruby project
	if _, err := os.Stat("Gemfile"); err == nil {
		return "Ruby project"
	}

	return ""
}

// extractStagedChangesSummary extracts a summary of staged changes from git diff stat
func extractStagedChangesSummary() string {
	// Try to get the commit message from .git/COMMIT_EDITMSG if a commit is in progress
	cmd := exec.Command("git", "diff", "--cached", "--stat")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	// Generate a summary from the stat
	statLines := strings.Split(string(output), "\n")
	if len(statLines) > 0 && statLines[0] != "" {
		// Extract file count and change summary
		return fmt.Sprintf("Staged changes summary: %s", strings.TrimSpace(statLines[0]))
	}

	return ""
}

// extractKeyCommentsFromDiff extracts important comments from the diff that explain WHY changes were made
func extractKeyCommentsFromDiff(diff string) string {
	lines := strings.Split(diff, "\n")
	var keyComments []string
	currentFile := ""

	for _, line := range lines {
		// Track current file
		if strings.HasPrefix(line, "diff --git") {
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				currentFile = strings.TrimPrefix(parts[3], "b/")
			}
			continue
		}

		// Look for added comments (lines starting with + that contain // or #)
		if strings.HasPrefix(line, "+") && (strings.Contains(line, "//") || strings.Contains(line, "#")) {
			// Extract comment content
			comment := strings.TrimPrefix(line, "+")
			comment = strings.TrimSpace(comment)

			// Filter for important comment patterns
			if isImportantComment(comment) {
				keyComments = append(keyComments, fmt.Sprintf("- %s: %s", currentFile, comment))
			}
		}
	}

	if len(keyComments) > 0 {
		// Limit to top 10 most important comments
		if len(keyComments) > 10 {
			keyComments = keyComments[:10]
		}
		return strings.Join(keyComments, "\n")
	}

	return ""
}

// isImportantComment determines if a comment is important enough to highlight
func isImportantComment(comment string) bool {
	commentUpper := strings.ToUpper(comment)

	// Keywords that indicate important context
	importantKeywords := []string{
		"CRITICAL", "IMPORTANT", "NOTE:", "WARNING", "TODO:", "FIXME",
		"HACK", "XXX", "BUG", "SECURITY", "FIX", "WORKAROUND",
		"BECAUSE", "REASON:", "WHY:", "INTENT:", "PURPOSE:",
	}

	for _, keyword := range importantKeywords {
		if strings.Contains(commentUpper, keyword) {
			return true
		}
	}

	// Multi-line comments are usually important
	if strings.HasPrefix(comment, "//") && len(comment) > 50 {
		return true
	}

	return false
}

// categorizeChanges analyzes the diff and categorizes the types of changes
func categorizeChanges(diff string) string {
	lines := strings.Split(diff, "\n")
	categories := make(map[string]int)

	for _, line := range lines {
		// Skip file headers
		if strings.HasPrefix(line, "diff --git") || strings.HasPrefix(line, "index") {
			continue
		}

		// Added lines
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			addedLine := strings.TrimPrefix(line, "+")

			// Security-related
			if strings.Contains(strings.ToUpper(addedLine), "SECURITY") ||
				strings.Contains(addedLine, "filesystem.ErrOutsideWorkingDirectory") ||
				strings.Contains(addedLine, "WithSecurityBypass") {
				categories["Security fixes/improvements"]++
			}

			// Error handling
			if strings.Contains(addedLine, "error") ||
				strings.Contains(addedLine, "Err") ||
				strings.Contains(addedLine, "return nil") ||
				strings.Contains(addedLine, "if err") {
				categories["Error handling"]++
			}

			// Documentation
			if strings.HasSuffix(strings.TrimSpace(addedLine), ".md") ||
				strings.Contains(strings.ToUpper(addedLine), "COMMENT") ||
				strings.Contains(strings.ToUpper(addedLine), "DOCUMENT") {
				categories["Documentation"]++
			}

			// Dependencies
			if strings.Contains(addedLine, "require(") ||
				strings.Contains(addedLine, "github.com/") ||
				strings.Contains(addedLine, "go.mod") {
				categories["Dependency updates"]++
			}

			// Tests
			if strings.Contains(addedLine, "Test") ||
				strings.Contains(addedLine, "test") {
				categories["Test changes"]++
			}

			// Debug/logging
			if strings.Contains(addedLine, "debugLog") ||
				strings.Contains(addedLine, "log.") ||
				strings.Contains(addedLine, "fmt.Print") {
				categories["Debug/logging"]++
			}
		}

		// Removed lines
		if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			categories["Code removal/refactoring"]++
		}
	}

	// Build summary
	if len(categories) == 0 {
		return ""
	}

	var categoryList []string
	for category, count := range categories {
		categoryList = append(categoryList, fmt.Sprintf("- %s (%d changes)", category, count))
	}

	return strings.Join(categoryList, "\n")
}

// extractFileContextForChanges extracts full file context for changed files
// This prevents false positives where the LLM thinks code is "missing" when it was just moved
func extractFileContextForChanges(diff string) string {
	lines := strings.Split(diff, "\n")
	changedFiles := make(map[string]bool)
	currentFile := ""

	// First pass: find all changed files
	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git") {
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				// Extract file path from "b/file.go"
				currentFile = strings.TrimPrefix(parts[3], "b/")
				changedFiles[currentFile] = true
			}
		}
	}

	// Note: We limit to 100 lines per file below, which keeps total context manageable
	// even with many changed files.

	// Second pass: extract context for each file
	var contextParts []string
	for filePath := range changedFiles {
		// SECURITY: Validate file path to prevent directory traversal attacks
		if !isValidRepoFilePath(filePath) {
			// Skip files outside repository
			continue
		}

		// Skip if file doesn't exist (deleted)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			continue
		}

		// Skip lock files, generated files, etc.
		if shouldSkipFileForContext(filePath) {
			continue
		}

		// Read file content
		content, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		// Add file context (limit to first 500 lines for better review accuracy)
		fileLines := strings.Split(string(content), "\n")
		maxLines := 500
		if len(fileLines) < maxLines {
			maxLines = len(fileLines)
		}

		if maxLines > 0 {
			contextParts = append(contextParts, fmt.Sprintf("### %s\n```go\n%s\n```", filePath, strings.Join(fileLines[:maxLines], "\n")))
		}
	}

	if len(contextParts) == 0 {
		return ""
	}

	return "## Full File Context\n" + strings.Join(contextParts, "\n\n")
}

// shouldSkipFileForContext determines if a file should be skipped for context extraction
func shouldSkipFileForContext(filePath string) bool {
	// Skip lock files
	if strings.HasSuffix(filePath, ".sum") ||
		strings.HasSuffix(filePath, ".lock") ||
		strings.HasSuffix(filePath, "package-lock.json") ||
		strings.HasSuffix(filePath, "yarn.lock") {
		return true
	}

	// Skip generated/minified files
	if strings.Contains(filePath, ".min.") ||
		strings.HasSuffix(filePath, ".map") ||
		strings.Contains(filePath, "node_modules/") {
		return true
	}

	// Skip protocol buffer generated files
	if strings.HasSuffix(filePath, ".pb.go") ||
		strings.HasSuffix(filePath, ".pb.cc") ||
		strings.HasSuffix(filePath, ".pb.h") ||
		strings.Contains(filePath, "_generated.go") ||
		strings.Contains(filePath, "_generated.") {
		return true
	}

	// Skip coverage and test output files
	if strings.HasSuffix(filePath, "coverage.out") ||
		strings.HasSuffix(filePath, "coverage.html") ||
		strings.HasSuffix(filePath, ".test") ||
		strings.HasSuffix(filePath, ".out") {
		return true
	}

	// Skip SVG and other binary generated assets
	if strings.HasSuffix(filePath, ".svg") ||
		strings.HasSuffix(filePath, ".png") ||
		strings.HasSuffix(filePath, ".jpg") ||
		strings.HasSuffix(filePath, ".ico") {
		return true
	}

	// Skip vendor directories
	if strings.Contains(filePath, "vendor/") ||
		strings.Contains(filePath, ".git/") {
		return true
	}

	return false
}

// isValidRepoFilePath validates that a file path is within the repository and safe to read
// Prevents directory traversal attacks when reading files from git diff output
func isValidRepoFilePath(filePath string) bool {
	// Prevent parent directory traversal
	if strings.Contains(filePath, "..") {
		return false
	}

	// Clean the path to resolve any symbolic links or relative components
	cleaned := filepath.Clean(filePath)

	// Convert to absolute path
	absPath, err := filepath.Abs(cleaned)
	if err != nil {
		return false
	}

	// Get current working directory (should be repository root)
	cwd, err := os.Getwd()
	if err != nil {
		return false
	}

	// Ensure the file is within the current directory/repo
	absCwd, err := filepath.Abs(cwd)
	if err != nil {
		return false
	}

	// Check that the file path starts with the current directory
	return strings.HasPrefix(absPath, absCwd)
}
