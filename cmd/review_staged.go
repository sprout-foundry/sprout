//go:build !js

package cmd

import (
	"context"
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
	reviewStagedProvider   string
	reviewStagedSkipPrompt bool // Not strictly necessary for review, but consistent with other commands
	reviewBase             string
)

var reviewStagedCmd = &cobra.Command{
	Use:   "review",
	Short: "Perform an AI-powered code review on staged changes or a branch",
	Long: `Review staged Git changes with an LLM: code quality, likely bugs, and
suggestions for improvement.

With --base, review the committed work on the current branch instead — the
same diff a pull request against that ref would show.

Examples:
  sprout review                 # staged changes
  sprout review --base main     # everything on this branch since main`,
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := utils.GetLogger(reviewStagedSkipPrompt)

		cfg, err := configuration.LoadOrInitConfig(reviewStagedSkipPrompt)
		if err != nil {
			return fmt.Errorf("failed to load or initialize config: %w", err)
		}

		// Override model if specified by flag
		var customAgentClient api.ClientInterface
		if reviewStagedModel != "" || reviewStagedProvider != "" {
			clientType, resolvedModel, err := configuration.ResolveProviderModel(cfg, reviewStagedProvider, reviewStagedModel)
			if err != nil {
				return fmt.Errorf("failed to resolve provider/model from '%s': %w", reviewStagedModel, err)
			}
			customAgentClient, err = factory.CreateProviderClient(clientType, resolvedModel)
			if err != nil {
				return fmt.Errorf("failed to create agent client with provider '%s' model '%s': %w", clientType, resolvedModel, err)
			}
			logger.LogProcessStep(fmt.Sprintf("Using custom provider/model: %s | %s", clientType, resolvedModel))
		}

		stagedDiff, err := reviewDiffSource(cmd, logger)
		if err != nil || stagedDiff == "" {
			return err
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

		// Create the unified code review service
		service := codereview.NewCodeReviewService(cfg, logger)

		// Use custom agent client if model flag was set, otherwise use default
		agentClient := customAgentClient
		if agentClient == nil {
			agentClient = service.GetDefaultAgentClient()
		}

		// Create the review context with metadata
		ctx := &codereview.ReviewContext{
			Diff:        reviewDiff,
			Config:      cfg,
			Logger:      logger,
			AgentClient: agentClient,
		}
		if reviewBase != "" {
			codereview.BuildRangeContext(context.Background(), "", reviewBase, stagedDiff).Apply(ctx)
		} else {
			codereview.BuildStagedContext(context.Background(), "", stagedDiff).Apply(ctx)
		}

		// Create review options for staged review
		opts := &codereview.ReviewOptions{
			Type:       codereview.StagedReview,
			SkipPrompt: reviewStagedSkipPrompt,
		}

		reviewResponse, err := service.PerformReview(ctx, opts)
		if err != nil {
			return fmt.Errorf("failed to get code review from LLM: %w", err)
		}

		logger.LogUserInteraction("\n--- Code Review ---")
		logger.LogUserInteraction(fmt.Sprintf("Status: %s", strings.ToUpper(reviewResponse.Status)))
		logger.LogUserInteraction(fmt.Sprintf("Feedback:\n%s", reviewResponse.Feedback))

		// If review needs revision and not in skip-prompt mode, offer agentic review
		if !reviewStagedSkipPrompt && (reviewResponse.Status == "needs_revision" || reviewResponse.Status == "rejected") {
			logger.LogUserInteraction("\n")
			prompt := "The review identified issues that need attention. Would you like to run a stricter evidence-focused review pass to filter out false positives? For a review that opens files to verify findings, use /review-deep in an interactive session. (yes/no): "

			if logger.AskForConfirmation(prompt, false, false) {
				agenticResponse, err := service.PerformAgenticReview(ctx, opts)
				if err != nil {
					logger.LogUserInteraction(fmt.Sprintf("Note: evidence-focused review failed (%v). Using initial review results.", err))
				} else {
					logger.LogUserInteraction("\n--- Evidence-Focused Review Results ---")
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
		return nil
	},
}

func init() {
	reviewStagedCmd.Flags().StringVarP(&reviewStagedModel, "model", "m", "", "Model for the review (e.g. 'ollama:llama3')")
	reviewStagedCmd.Flags().StringVarP(&reviewStagedProvider, "provider", "p", "", providerFlagUsage)
	reviewStagedCmd.Flags().BoolVarP(&reviewStagedSkipPrompt, "yes", "y", false, "Skip interactive prompts")
	reviewStagedCmd.Flags().StringVar(&reviewBase, "base", "", "Review commits on this branch since <ref> instead of staged changes")
	boolFlagAlias(reviewStagedCmd.Flags(), &reviewStagedSkipPrompt, "skip-prompt", "yes", aliasDeprecated)
}

// reviewDiffSource returns the diff to review: base...HEAD with --base, the
// staged changes otherwise. An empty diff with a nil error means there is
// nothing to review and the user has been told.
func reviewDiffSource(cmd *cobra.Command, logger *utils.Logger) (string, error) {
	if reviewBase != "" {
		diff, err := codereview.RangeDiff(context.Background(), "", reviewBase)
		if err != nil {
			return "", usageErrorf(cmd, "%v", err)
		}
		if strings.TrimSpace(diff) == "" {
			logger.LogUserInteraction(fmt.Sprintf("No changes on this branch since %s. Nothing to review.", reviewBase))
			return "", nil
		}
		logger.LogProcessStep(fmt.Sprintf("Reviewing changes since %s...", reviewBase))
		return diff, nil
	}

	if err := exec.Command("git", "diff", "--cached", "--quiet", "--exit-code").Run(); err == nil {
		logger.LogUserInteraction("No staged changes found. Stage changes, or pass --base <ref> to review a branch.")
		return "", nil
	} else if _, ok := err.(*exec.ExitError); !ok {
		return "", fmt.Errorf("failed to check for staged changes: %w", err)
	}
	logger.LogProcessStep("Staged changes detected. Performing code review...")

	out, err := exec.Command("git", "diff", "--cached").Output()
	if err != nil {
		return "", fmt.Errorf("failed to get staged diff: %w", err)
	}
	if strings.TrimSpace(string(out)) == "" {
		logger.LogUserInteraction("No actual diff content found in staged changes. Nothing to review.")
		return "", nil
	}
	return string(out), nil
}
