package editor

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/orchestration/types" // Added for OrchestrationPlan type
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/alantheprice/ledit/pkg/workspace" // Added for workspace context
)

type RequirementProcessor struct {
	cfg *config.Config
}

func NewRequirementProcessor(cfg *config.Config) *RequirementProcessor {
	return &RequirementProcessor{cfg: cfg}
}

func (p *RequirementProcessor) Process(plan *types.OrchestrationPlan) error {
	logger := utils.GetLogger(true) // Get the logger instance
	execCfg := *p.cfg
	execCfg.SkipPrompt = true

	maxAttempts := p.cfg.OrchestrationMaxAttempts

	for i := range plan.Requirements {
		req := &plan.Requirements[i]

		if req.Status == "completed" {
			logger.LogProcessStep(prompts.SkippingCompletedStep(req.Instruction)) // Use prompt
			continue
		}

		logger.LogProcessStep(prompts.ExecutingStep(req.Instruction)) // Use prompt

		// Step 1: Get file-specific changes for the high-level requirement
		// Regenerate changes if not already present or if previous attempt failed
		if len(req.Changes) == 0 || req.Status == "failed" {
			logger.LogProcessStep(prompts.GeneratingFileChanges(req.Instruction))
			// Get workspace context for the LLM to generate relevant changes
			workspaceContext := workspace.GetWorkspaceContext(req.Instruction, p.cfg)
			changes, err := llm.GetChangesForRequirement(p.cfg, req.Instruction, workspaceContext)
			if err != nil {
				req.Status = "failed"
				saveOrchestrationPlan(plan)
				logger.LogProcessStep(prompts.GenerateChangesFailed(req.Instruction, err))
				return fmt.Errorf("failed to generate file changes for requirement '%s': %w", req.Instruction, err)
			}
			req.Changes = changes
			// Initialize status for new changes
			for j := range req.Changes {
				req.Changes[j].Status = "pending"
			}
			saveOrchestrationPlan(plan) // Save the plan with generated changes
		}

		// Step 2: Process each file-specific change
		var requirementFailed bool
		for j := range req.Changes {
			change := &req.Changes[j]

			if change.Status == "completed" {
				logger.LogProcessStep(prompts.SkippingCompletedFileChange(change.Filepath, change.Instruction))
				continue
			}

			logger.LogProcessStep(prompts.ProcessingFile(change.Filepath))
			logger.LogProcessStep(prompts.ExecutingFileChange(change.Instruction))

			var lastValidationErr error

			for attempt := 1; attempt <= maxAttempts; attempt++ {
				if attempt > 1 {
					logger.LogProcessStep(prompts.RetryAttempt(attempt, maxAttempts, change.Instruction))
				}

				currentInstruction := p.getCurrentInstructionForAttempt(change.Instruction, change.Filepath, change.ValidationFailureContext, change.LastLLMResponse)

				processedInstruction, _, err := processInstructions(currentInstruction, p.cfg)
				if err != nil {
					change.Status = "failed"
					change.LastLLMResponse = ""
					change.ValidationFailureContext = fmt.Sprintf("Failed to process instruction: %v", err)
					saveOrchestrationPlan(plan)
					logger.LogProcessStep(prompts.ProcessInstructionFailed(change.Filepath, err))
					lastValidationErr = err // Mark this as the reason for failure
					break
				}

				diffForTargetFile, err := ProcessCodeGeneration(change.Filepath, processedInstruction, &execCfg)
				if err != nil {
					change.Status = "failed"
					change.LastLLMResponse = diffForTargetFile
					change.ValidationFailureContext = fmt.Sprintf("Code generation failed: %v", err)
					saveOrchestrationPlan(plan)
					logger.LogProcessStep(prompts.ProcessRequirementFailed(change.Filepath, err))
					continue // Continue to next attempt
				}

				// Always run setup.sh and validate.sh after each code generation step
				setupErr := createAndRunSetupScript(change.Instruction, change.Filepath, &execCfg) // Pass change details
				if setupErr != nil {
					logger.LogProcessStep(prompts.SetupFailedAttempt(attempt, setupErr))
					change.Status = "failed"
					change.ValidationFailureContext = prompts.ValidationFailureContextSetupScriptFailed(setupErr)
					change.LastLLMResponse = diffForTargetFile
					saveOrchestrationPlan(plan)
					lastValidationErr = setupErr
					continue // Continue to next attempt
				}

				lastValidationErr = createAndRunValidationScript(change.Instruction, change.Filepath, &execCfg) // Pass change details
				if lastValidationErr != nil {
					logger.LogProcessStep(prompts.ValidationFailedAttempt(attempt, lastValidationErr))
					change.Status = "failed"
					change.ValidationFailureContext = prompts.ValidationFailureContextValidationScriptFailed(lastValidationErr)
					change.LastLLMResponse = diffForTargetFile
					saveOrchestrationPlan(plan)
					continue // Continue to next attempt
				}

				change.Status = "completed"
				change.ValidationFailureContext = ""
				change.LastLLMResponse = ""
				if err := saveOrchestrationPlan(plan); err != nil {
					logger.LogProcessStep(prompts.SaveProgressFailed(change.Filepath, err))
					return fmt.Errorf("file change for %s completed, but failed to save progress: %w", change.Filepath, err)
				}
				logger.LogProcessStep(prompts.FileChangeCompleted(change.Filepath, change.Instruction))
				break // Break from attempt loop, this change is completed
			}

			if lastValidationErr != nil {
				logger.LogProcessStep(prompts.FileChangeFailedAfterAttempts(change.Filepath, change.Instruction, maxAttempts, lastValidationErr))
				requirementFailed = true
				break // Break from changes loop, mark requirement as failed
			}
		}

		if requirementFailed {
			req.Status = "failed"
			// The ValidationFailureContext and LastLLMResponse for the *requirement* itself
			// are less relevant now, as the failure context is on the specific change.
			// We could aggregate, but for now, just mark the requirement failed.
			if err := saveOrchestrationPlan(plan); err != nil {
				logger.LogProcessStep(prompts.SaveProgressFailed("requirement", err))
				return fmt.Errorf("requirement '%s' failed, but failed to save progress: %w", req.Instruction, err)
			}
			return fmt.Errorf("requirement '%s' failed due to issues with its file changes", req.Instruction)
		}

		req.Status = "completed"
		if err := saveOrchestrationPlan(plan); err != nil {
			logger.LogProcessStep(prompts.SaveProgressFailed("requirement", err))
			return fmt.Errorf("requirement '%s' completed, but failed to save progress: %w", req.Instruction, err)
		}
		logger.LogProcessStep(prompts.StepCompleted(req.Instruction))
	}

	logger.LogProcessStep(prompts.AllOrchestrationStepsCompleted())
	return nil
}

// getCurrentInstructionForAttempt now takes specific instruction and file details for a change.
func (p *RequirementProcessor) getCurrentInstructionForAttempt(instruction, filepath, validationFailureContext, lastLLMResponse string) string {
	logger := utils.GetLogger(true)
	if validationFailureContext != "" {
		re := regexp.MustCompile(`(?i)\s*#WS\s*$`)
		originalInstructionWithoutTag := strings.TrimSpace(re.ReplaceAllString(instruction, ""))

		var contextPrompt string
		var generatedSearchQueries []string

		searchContext := fmt.Sprintf(
			"Error: %s\nContext: %s",
			validationFailureContext, originalInstructionWithoutTag,
		)

		queries, err := llm.GenerateSearchQuery(p.cfg, searchContext)
		if err == nil && len(queries) > 0 {
			generatedSearchQueries = queries
			for _, q := range generatedSearchQueries {
				logger.LogProcessStep(prompts.GeneratedSearchQuery(q))
			}
		} else {
			logger.LogProcessStep(prompts.SearchQueryGenerationWarning(err))
		}

		if lastLLMResponse != "" {
			contextPrompt = prompts.RetryPromptWithDiff(originalInstructionWithoutTag, filepath, validationFailureContext, lastLLMResponse)
		} else {
			contextPrompt = prompts.RetryPromptWithoutDiff(originalInstructionWithoutTag, filepath, validationFailureContext)
		}

		if len(generatedSearchQueries) > 0 {
			var sgTags strings.Builder
			for _, query := range generatedSearchQueries {
				sgTags.WriteString(fmt.Sprintf("#SG \"%s\"\n", query))
				logger.LogProcessStep(prompts.AddedSearchGrounding(query))
			}
			contextPrompt = sgTags.String() + contextPrompt
		}

		logger.LogProcessStep(prompts.AddingValidationFailureContext())
		return contextPrompt
	}
	return instruction
}
