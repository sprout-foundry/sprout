package editor

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts" // Import the new prompts package
	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/alantheprice/ledit/pkg/workspace"
	"os"
	"path/filepath"
	"strings"
)

// OrchestrateFeature is the main entry point for the orchestrate command.
func OrchestrateFeature(prompt string, cfg *config.Config) error {
	logger := utils.GetLogger(cfg.SkipPrompt) // Get the logger instance

	rp := NewRequirementProcessor(cfg)
	// Ensure .ledit directory exists
	if err := os.MkdirAll(filepath.Dir(requirementsFile), os.ModePerm); err != nil {
		logger.LogProcessStep(prompts.LeditDirCreationError(err)) // Use prompt
		return fmt.Errorf("could not create .ledit directory: %w", err)
	}

	plan, err := loadOrchestrationPlan()
	if err == nil && len(plan.Requirements) > 0 && hasPendingRequirements(plan) {
		if cfg.SkipPrompt {
			logger.LogProcessStep(prompts.UnfinishedPlanAutoResume()) // Use prompt
			return rp.Process(plan)
		}

		logger.LogProcessStep(prompts.UnfinishedPlanFound())                         // Use prompt
		if logger.AskForConfirmation(prompts.ContinueOrchestrationPrompt(), false) { // Use prompt
			logger.LogProcessStep(prompts.ResumingOrchestration()) // Use prompt
			return rp.Process(plan)
		}
	}

	logger.LogProcessStep(prompts.GeneratingNewPlan()) // Use prompt
	newPlan, err := generateRequirements(prompt, cfg)
	if err != nil {
		logger.LogProcessStep(prompts.GenerateRequirementsFailed(err)) // Use prompt
		return fmt.Errorf("failed to generate requirements: %w", err)
	}

	if newPlan == nil || len(newPlan.Requirements) == 0 {
		logger.LogProcessStep(prompts.EmptyOrchestrationPlan()) // Use prompt
		return nil
	}

	logger.LogProcessStep(prompts.GeneratedPlanHeader()) // Use prompt
	for i, req := range newPlan.Requirements {
		logger.Logf(prompts.PlanStep(i, req.Filepath, req.Instruction)) // Use prompt
	}

	if !cfg.SkipPrompt {
		fmt.Print(prompts.ApplyPlanPrompt()) // Use prompt
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		if strings.TrimSpace(strings.ToLower(input)) != "y" {
			logger.LogProcessStep(prompts.OrchestrationCancelled()) // Use prompt
			return nil
		}
	}

	return rp.Process(newPlan)
}

func hasPendingRequirements(plan *OrchestrationPlan) bool {
	for _, req := range plan.Requirements {
		if req.Status == "pending" || req.Status == "failed" {
			return true
		}
	}
	return false
}

func generateRequirements(prompt string, cfg *config.Config) (*OrchestrationPlan, error) {
	// Process the initial prompt for search grounding or workspace context
	processedPrompt, _, err := processInstructions(prompt, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to process initial prompt for orchestration: %w", err)
	}

	workspaceContext := workspace.GetWorkspaceContext(processedPrompt, cfg)

	response, err := llm.GetOrchestrationPlan(cfg, processedPrompt, workspaceContext)
	if err != nil {
		return nil, fmt.Errorf("failed to get requirements from LLM: %w", err)
	}

	if response == "" {
		return nil, fmt.Errorf("LLM returned an empty plan")
	}

	var plan OrchestrationPlan
	if err := json.Unmarshal([]byte(response), &plan); err != nil {
		return nil, fmt.Errorf("failed to parse requirements JSON from LLM response: %w\nResponse was: %s", err, response)
	}

	if err := saveOrchestrationPlan(&plan); err != nil {
		return nil, fmt.Errorf("failed to save orchestration plan: %w", err)
	}

	return &plan, nil
}
