package cmd

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/alantheprice/ledit/pkg/codereview"
	"github.com/alantheprice/ledit/pkg/config"
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

		cfg, err := config.LoadOrInitConfig(reviewStagedSkipPrompt)
		if err != nil {
			logger.LogError(fmt.Errorf("failed to load or initialize config: %w", err))
			return
		}

		// Override model if specified by flag
		if reviewStagedModel != "" {
			cfg.EditingModel = reviewStagedModel // Use EditingModel for review tasks
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
			logger.LogUserInteraction("No staged changes found. Please stage your changes before running 'ledit review-staged'.")
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
			SkipPrompt:       reviewStagedSkipPrompt,
			AutoApplyFixes:   false, // Don't auto-apply fixes for staged reviews
			RollbackOnReject: false, // Don't rollback for staged reviews
		}

		logger.LogProcessStep("Sending staged changes to LLM for review...")
		reviewResponse, err := service.PerformReview(ctx, opts)
		if err != nil {
			logger.LogError(fmt.Errorf("failed to get code review from LLM: %w", err))
			return
		}

		logger.LogUserInteraction("\n--- AI Code Review ---")
		logger.LogUserInteraction(fmt.Sprintf("Status: %s", strings.ToUpper(reviewResponse.Status)))
		logger.LogUserInteraction(fmt.Sprintf("Feedback:\n%s", reviewResponse.Feedback))

		if reviewResponse.Status == "rejected" && reviewResponse.NewPrompt != "" {
			logger.LogUserInteraction(fmt.Sprintf("\nSuggested New Prompt for Re-execution:\n%s", reviewResponse.NewPrompt))
		}
		logger.LogUserInteraction("----------------------")
	},
}

func init() {
	reviewStagedCmd.Flags().StringVarP(&reviewStagedModel, "model", "m", "", "Specify the LLM model to use for the code review (e.g., 'ollama:llama3')")
	reviewStagedCmd.Flags().BoolVar(&reviewStagedSkipPrompt, "skip-prompt", false, "Skip any interactive prompts (e.g., for confirmation, though less relevant for review)")
	rootCmd.AddCommand(reviewStagedCmd)
}
