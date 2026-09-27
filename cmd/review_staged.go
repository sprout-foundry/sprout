//go:build !js

package cmd

import (
	"fmt"
	"os/exec"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/codereview"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/factory"
	"github.com/sprout-foundry/sprout/pkg/utils"

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
			clientType, resolvedModel, err := configuration.ResolveProviderModel(cfg, "", reviewStagedModel)
			if err != nil {
				logger.LogError(fmt.Errorf("failed to resolve provider/model from '%s': %w", reviewStagedModel, err))
				return
			}
			customAgentClient, err = factory.CreateProviderClient(clientType, resolvedModel)
			if err != nil {
				logger.LogError(fmt.Errorf("failed to create agent client with provider '%s' model '%s': %w", clientType, resolvedModel, err))
				return
			}
			logger.LogProcessStep(fmt.Sprintf("Using custom provider/model: %s | %s", clientType, resolvedModel))
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
			logger.LogUserInteraction("No staged changes found. Please stage your changes before running 'sprout review'.")
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

		logger.LogUserInteraction("\n--- Code Review ---")
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
