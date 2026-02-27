package commands

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/alantheprice/ledit/pkg/agent"
	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/codereview"
	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/factory"
	"github.com/alantheprice/ledit/pkg/types"
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
	return runReviewCommand("review", false, args, chatAgent)
}

// ReviewDeepCommand implements the /review-deep slash command
// This command performs a deeper evidence-focused review on staged Git changes
type ReviewDeepCommand struct{}

// Name returns the command name
func (c *ReviewDeepCommand) Name() string {
	return "review-deep"
}

// Description returns the command description
func (c *ReviewDeepCommand) Description() string {
	return "Perform deep evidence-based code review on staged Git changes"
}

// Execute runs the deep code review command
func (c *ReviewDeepCommand) Execute(args []string, chatAgent *agent.Agent) error {
	return runReviewCommand("review-deep", true, args, chatAgent)
}

func runReviewCommand(commandName string, deepReview bool, args []string, chatAgent *agent.Agent) error {
	// Create a logger (skip prompt for non-interactive mode)
	logger := utils.GetLogger(true)

	if deepReview {
		logger.LogProcessStep("Starting deep code review of staged changes...")
	} else {
		logger.LogProcessStep("Starting code review of staged changes...")
	}

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
			if deepReview {
				logger.LogProcessStep("Staged changes detected. Performing deep review...")
			} else {
				logger.LogProcessStep("Staged changes detected. Performing code review...")
			}
		} else {
			logger.LogError(fmt.Errorf("failed to check for staged changes: %w", err))
			return fmt.Errorf("git error: failed to check for staged changes: %v", err)
		}
	} else {
		logger.LogUserInteraction(fmt.Sprintf("No staged changes found. Please stage your changes before running '/%s'.", commandName))
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

	// Optimize diff for more efficient API usage
	// NOTE: Disable aggressive optimization for code review to ensure proper context
	optimizer := utils.NewDiffOptimizerForReview()
	optimizedDiff := optimizer.OptimizeDiff(stagedDiff)

	logger.LogProcessStep(fmt.Sprintf("Optimized diff: %d -> %d lines, %d bytes saved",
		optimizedDiff.OriginalLines, optimizedDiff.OptimizedLines, optimizedDiff.BytesSaved))

	// Create the unified code review service
	service := codereview.NewCodeReviewService(cfg, logger)

	// Create the review context with optimized diff and metadata.
	// IMPORTANT: Must include AgentClient for the review to work.
	agentClient := service.GetDefaultAgentClient()
	if chatAgent != nil {
		activeProvider := strings.TrimSpace(chatAgent.GetProvider())
		activeModel := strings.TrimSpace(chatAgent.GetModel())
		if activeProvider != "" {
			if sessionClient, err := factory.CreateProviderClient(api.ClientType(activeProvider), activeModel); err == nil {
				agentClient = sessionClient
				logger.LogProcessStep(fmt.Sprintf("Using active session provider/model for review: %s | %s", activeProvider, activeModel))
			} else {
				logger.LogProcessStep(fmt.Sprintf("Warning: failed to use active session provider/model (%s | %s): %v", activeProvider, activeModel, err))
			}
		}
	}
	if agentClient == nil {
		logger.LogError(fmt.Errorf("failed to get default agent client"))
		return fmt.Errorf("agent client initialization failed")
	}

	reviewCtx := &codereview.ReviewContext{
		Diff:             optimizedDiff.OptimizedContent,
		Config:           cfg,
		Logger:           logger,
		AgentClient:      agentClient,
		ProjectType:      detectProjectType(),
		CommitMessage:    extractStagedChangesSummary(),
		KeyComments:      extractKeyCommentsFromDiff(stagedDiff),
		ChangeCategories: categorizeChanges(stagedDiff),
		FullFileContext:  extractFileContextForChanges(stagedDiff),
	}

	// Add file summaries to context if available
	if len(optimizedDiff.FileSummaries) > 0 {
		var summaryInfo strings.Builder
		summaryInfo.WriteString("\n\nLarge files optimized for review:\n")
		for file, summary := range optimizedDiff.FileSummaries {
			summaryInfo.WriteString(fmt.Sprintf("- %s: %s\n", file, summary))
		}
		reviewCtx.Diff += summaryInfo.String()
	}

	// Create review options for staged review
	opts := &codereview.ReviewOptions{
		Type:             codereview.StagedReview,
		SkipPrompt:       true,  // Skip prompts for slash command
		RollbackOnReject: false, // Don't rollback for staged reviews
	}

	var reviewResponse *types.CodeReviewResult
	if deepReview {
		logger.LogProcessStep("Sending staged changes to LLM for deep review...")
		reviewResponse, err = service.PerformAgenticReview(reviewCtx, opts)
	} else {
		logger.LogProcessStep("Sending staged changes to LLM for review...")
		reviewResponse, err = service.PerformReview(reviewCtx, opts)
	}
	if err != nil {
		logger.LogError(fmt.Errorf("failed to get code review from LLM: %w", err))
		return fmt.Errorf("LLM error: failed to perform %s: %v", commandName, err)
	}

	if deepReview {
		logger.LogProcessStep("Deep code review completed successfully")
	} else {
		logger.LogProcessStep("Code review completed successfully")
	}

	header := "📋 AI CODE REVIEW"
	if deepReview {
		header = "📋 AI CODE REVIEW (DEEP PASS)"
	}

	// Build review output string for conversation history
	reviewOutput := fmt.Sprintf("%s\n%s\n\nStatus: %s\n\nFeedback:\n%s",
		header,
		strings.Repeat("═", 50),
		strings.ToUpper(reviewResponse.Status),
		reviewResponse.Feedback)

	if strings.TrimSpace(reviewResponse.DetailedGuidance) != "" {
		reviewOutput += fmt.Sprintf("\n\nDetailed Guidance:\n%s", reviewResponse.DetailedGuidance)
	}

	if reviewResponse.Status == "rejected" && reviewResponse.NewPrompt != "" {
		reviewOutput += fmt.Sprintf("\n\nSuggested New Prompt:\n%s", reviewResponse.NewPrompt)
	}

	// Add review request and output to conversation history so the agent can chat about it
	if chatAgent != nil {
		// Add user message representing the review request
		chatAgent.AddMessage(api.Message{
			Role:    "user",
			Content: "/" + commandName + " " + strings.Join(args, " "),
		})

		// Add assistant message containing the review output
		chatAgent.AddMessage(api.Message{
			Role:    "assistant",
			Content: reviewOutput,
		})
	}

	// Output the review using simple, reliable formatting with proper raw mode line endings
	fmt.Print("\r\n" + strings.Repeat("═", 50) + "\r\n")
	fmt.Print(header + "\r\n")
	fmt.Print(strings.Repeat("═", 50) + "\r\n\r\n")

	fmt.Printf("Status: %s\r\n\r\n", strings.ToUpper(reviewResponse.Status))

	fmt.Print("Feedback:\r\n")
	fmt.Print(strings.Repeat("-", 30) + "\r\n")
	// Convert any \n in the feedback to \r\n for raw mode compatibility
	feedback := strings.ReplaceAll(reviewResponse.Feedback, "\n", "\r\n")
	fmt.Print(feedback + "\r\n")

	if strings.TrimSpace(reviewResponse.DetailedGuidance) != "" {
		fmt.Print("\r\nDetailed Guidance:\r\n")
		fmt.Print(strings.Repeat("-", 30) + "\r\n")
		details := strings.ReplaceAll(reviewResponse.DetailedGuidance, "\n", "\r\n")
		fmt.Print(details + "\r\n")
	}

	if reviewResponse.Status == "rejected" && reviewResponse.NewPrompt != "" {
		fmt.Print("\r\nSuggested New Prompt:\r\n")
		fmt.Print(strings.Repeat("-", 30) + "\r\n")
		// Convert any \n in the new prompt to \r\n for raw mode compatibility
		newPrompt := strings.ReplaceAll(reviewResponse.NewPrompt, "\n", "\r\n")
		fmt.Print(newPrompt + "\r\n")
	}

	fmt.Print("\r\n" + strings.Repeat("═", 50) + "\r\n")

	return nil
}
