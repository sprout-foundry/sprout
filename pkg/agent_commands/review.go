package commands

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/codereview"
	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/utils"
)

// ReviewCommand implements the /review slash command
// This command performs AI-powered code review on staged Git changes
// It provides the same functionality as the 'ledit review' CLI command
// but accessible through the interactive agent console

type ReviewCommand struct{}

// Name returns the command name
func (c *ReviewCommand) Name() string {
	return "review"
}

// Description returns the command description
func (c *ReviewCommand) Description() string {
	return "Perform AI-powered code review on staged Git changes"
}

// Execute runs the code review command
func (c *ReviewCommand) Execute(args []string, chatAgent *agent.Agent) error {
	// Create a logger (skip prompt for non-interactive mode)
	logger := utils.GetLogger(true)

	logger.LogProcessStep("Starting code review of staged changes...")

	// Load configuration
	cfg, err := configuration.LoadOrInitConfig(true)
	if err != nil {
		logger.LogError(fmt.Errorf("failed to load or initialize config: %w", err))
		return fmt.Errorf("configuration error: %v", err)
	}

	logger.LogProcessStep("Configuration loaded successfully")

	// Check for staged changes
	cmdCheckStaged := exec.Command("git", "diff", "--cached", "--quiet", "--exit-code")
	if err := cmdCheckStaged.Run(); err != nil {
		// If err is not nil, it means there are staged changes (exit code 1) or another error
		if _, ok := err.(*exec.ExitError); ok {
			// ExitError means git exited with a non-zero status, which is what we want for staged changes
			logger.LogProcessStep("Staged changes detected. Performing code review...")
		} else {
			logger.LogError(fmt.Errorf("failed to check for staged changes: %w", err))
			return fmt.Errorf("git error: failed to check for staged changes: %v", err)
		}
	} else {
		logger.LogUserInteraction("No staged changes found. Please stage your changes before running '/review'.")
		return fmt.Errorf("no staged changes found")
	}

	// Get the diff of staged changes
	cmdDiff := exec.Command("git", "diff", "--cached")
	stagedDiffBytes, err := cmdDiff.Output()
	if err != nil {
		logger.LogError(fmt.Errorf("failed to get staged diff: %w", err))
		return fmt.Errorf("git error: failed to get staged diff: %v", err)
	}
	stagedDiff := string(stagedDiffBytes)

	if strings.TrimSpace(stagedDiff) == "" {
		logger.LogUserInteraction("No actual diff content found in staged changes. Nothing to review.")
		return fmt.Errorf("no diff content found")
	}

	logger.LogProcessStep(fmt.Sprintf("Retrieved staged diff (%d bytes)", len(stagedDiff)))

	// Create the unified code review service
	service := codereview.NewCodeReviewService(cfg, logger)

	// Create the review context
	ctx := &codereview.ReviewContext{
		Diff:   stagedDiff,
		Config: cfg,
		Logger: logger,
	}

	// Create review options for staged review
	opts := &codereview.ReviewOptions{
		Type:             codereview.StagedReview,
		SkipPrompt:       true,  // Skip prompts for slash command
		RollbackOnReject: false, // Don't rollback for staged reviews
	}

	logger.LogProcessStep("Sending staged changes to LLM for review...")
	reviewResponse, err := service.PerformReview(ctx, opts)
	if err != nil {
		logger.LogError(fmt.Errorf("failed to get code review from LLM: %w", err))
		return fmt.Errorf("LLM error: failed to perform code review: %v", err)
	}

	logger.LogProcessStep("Code review completed successfully")

	// Output the review using simple, reliable formatting (same pattern as test)
	fmt.Print("\n" + strings.Repeat("═", 50) + "\n")
	fmt.Print("📋 AI CODE REVIEW\n")
	fmt.Print(strings.Repeat("═", 50) + "\n\n")

	fmt.Printf("Status: %s\n\n", strings.ToUpper(reviewResponse.Status))

	fmt.Print("Feedback:\n")
	fmt.Print(strings.Repeat("-", 30) + "\n")
	fmt.Print(reviewResponse.Feedback + "\n")

	if reviewResponse.Status == "rejected" && reviewResponse.NewPrompt != "" {
		fmt.Print("\nSuggested New Prompt:\n")
		fmt.Print(strings.Repeat("-", 30) + "\n")
		fmt.Print(reviewResponse.NewPrompt + "\n")
	}

	fmt.Print("\n" + strings.Repeat("═", 50) + "\n")

	return nil
}
