package cmd

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
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

		// Prepare prompt and context for the LLM review
		reviewPrompt := prompts.CodeReviewStagedPrompt()
		// For workspace context, we can pass an empty string or try to get context from staged files.
		// For simplicity, let's start with an empty workspace context for now, as the diff itself is the primary context.
		workspaceContext := ""

		logger.LogProcessStep("Sending staged changes to LLM for review...")
		reviewResponse, err := llm.GetStagedCodeReview(cfg, stagedDiff, reviewPrompt, workspaceContext)
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
