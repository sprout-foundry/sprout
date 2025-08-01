package orchestration

import (
	"bytes" // Added import for bytes package
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/editor" // NEW IMPORT: Import editor package for ProcessCodeGeneration
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/orchestration/types"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/alantheprice/ledit/pkg/workspace"
)

// OrchestrateFeature is the main entry point for the orchestrate command.
// It generates a high-level plan and then processes each requirement.
func OrchestrateFeature(prompt string, cfg *config.Config) error {
	logger := utils.GetLogger(cfg.SkipPrompt)

	// 1. Generate the initial orchestration plan
	plan, err := generateRequirements(prompt, cfg)
	if err != nil {
		return fmt.Errorf("failed to generate orchestration plan: %w", err)
	}

	// Save the initial plan
	if err := SaveOrchestrationPlan(plan); err != nil { // Use SaveOrchestrationPlan from current package
		logger.LogError(fmt.Errorf("failed to save initial orchestration plan: %w", err))
	}

	logger.LogProcessStep("Orchestration plan generated:")
	for i, req := range plan.Requirements {
		logger.LogProcessStep(fmt.Sprintf("  %d. [Status: %s] %s", i+1, req.Status, req.Instruction))
	}

	// 2. Process each requirement
	for i := plan.CurrentStep; i < len(plan.Requirements); i++ {
		req := &plan.Requirements[i] // Get a pointer to modify the original slice element
		if req.Status == "completed" {
			continue
		}

		logger.LogProcessStep(fmt.Sprintf("\nProcessing requirement %d/%d: %s", i+1, len(plan.Requirements), req.Instruction))
		req.Status = "in_progress"
		plan.CurrentStep = i
		if err := SaveOrchestrationPlan(plan); err != nil { // Use SaveOrchestrationPlan from current package
			logger.LogError(fmt.Errorf("failed to save orchestration plan during processing: %w", err))
		}

		// Get workspace context for the current requirement
		workspaceContext := workspace.GetWorkspaceContext(req.Instruction, cfg)

		// Ask LLM to break down requirement into file-specific changes
		// The cfg object is passed, and GetChangesForRequirement will internally use cfg.Interactive
		changes, err := llm.GetChangesForRequirement(cfg, req.Instruction, workspaceContext)
		if err != nil {
			req.Attempts++
			req.LastError = err.Error()
			req.Status = "failed"
			logger.LogError(fmt.Errorf("failed to get changes for requirement '%s': %w", req.Instruction, err))
			if err := SaveOrchestrationPlan(plan); err != nil { // Use SaveOrchestrationPlan from current package
				logger.LogError(fmt.Errorf("failed to save orchestration plan after requirement failure: %w", err))
			}
			continue // Move to next requirement or retry if logic allows
		}

		req.Changes = changes // Store the generated changes
		logger.LogProcessStep(fmt.Sprintf("Generated %d file changes for requirement '%s'.", len(req.Changes), req.Instruction))

		// Apply each change
		allChangesApplied := true
		for j, change := range req.Changes {
			logger.LogProcessStep(fmt.Sprintf("  Applying change %d/%d for file: %s", j+1, len(req.Changes), change.Filepath))
			// The ProcessCodeGeneration function handles loading, LLM call, diff, user prompt, saving, and git commit
			// It expects instructions and an optional filename.
			// The change.Instruction should be specific enough for ProcessCodeGeneration.
			_, err := editor.ProcessWorkspaceCodeGeneration(change.Filepath, change.Instruction, cfg)
			if err != nil {
				change.Status = "failed"
				change.Error = err.Error()
				logger.LogError(fmt.Errorf("failed to apply change to %s: %w", change.Filepath, err))
				allChangesApplied = false
			} else {
				change.Status = "applied"
				logger.LogProcessStep(fmt.Sprintf("  Successfully applied change to %s.", change.Filepath))
			}
			req.Changes[j] = change                             // Update the change in the slice
			if err := SaveOrchestrationPlan(plan); err != nil { // Use SaveOrchestrationPlan from current package
				logger.LogError(fmt.Errorf("failed to save orchestration plan after applying change: %w", err))
			}
		}

		if allChangesApplied {
			req.Status = "completed"
			logger.LogProcessStep(fmt.Sprintf("Requirement '%s' completed successfully.", req.Instruction))
		} else {
			req.Status = "failed"
			logger.LogProcessStep(fmt.Sprintf("Requirement '%s' failed to complete all changes.", req.Instruction))
		}

		if err := SaveOrchestrationPlan(plan); err != nil { // Use SaveOrchestrationPlan from current package
			logger.LogError(fmt.Errorf("failed to save orchestration plan after requirement completion: %w", err))
		}
	}

	if !hasPendingRequirements(plan) {
		maxValidationAttempts := 7
		validationSuccess := false
		for attempt := 1; attempt <= maxValidationAttempts; attempt++ {
			logger.LogProcessStep(fmt.Sprintf("\nAttempt %d/%d: Validating changes by running build command...", attempt, maxValidationAttempts))
			plan.Attempts = attempt // Update attempts on the plan
			validationErr := validateChanges()
			if validationErr == nil {
				logger.LogProcessStep("Validation successful!")
				validationSuccess = true
				break // Validation passed, exit retry loop
			}

			logger.LogError(fmt.Errorf("validation failed: %w", validationErr))
			plan.LastError = validationErr.Error()

			if attempt < maxValidationAttempts {
				logger.LogProcessStep("Attempting to fix validation errors with LLM...")
				// The fix prompt should be general, as the validation failure might not be tied to a single file.
				// The #WS tag ensures workspace context is included.
				fixInstructions := fmt.Sprintf("The previous attempt to complete the orchestration failed validation. The build command failed with the following error:\n-------\n%s\n-------\n Please fix the code to resolve this issue. #WS", validationErr.Error())

				// Use ProcessCodeGeneration to attempt a fix.
				// Pass an empty filename as the fix might involve multiple files.
				_, fixErr := editor.ProcessCodeGeneration("", fixInstructions, cfg)
				if fixErr != nil {
					logger.LogError(fmt.Errorf("error during LLM fix attempt %d: %w", attempt, fixErr))
					// If the fix itself fails, we still continue to the next validation attempt
					// or exit if max attempts reached.
				} else {
					logger.LogProcessStep(fmt.Sprintf("LLM fix attempt %d completed. Retrying validation...", attempt))
				}
			} else {
				plan.Status = "failed" // Mark as failed if max attempts reached
				logger.LogProcessStep(fmt.Sprintf("\nMax validation attempts (%d) reached. Orchestration failed.", maxValidationAttempts))
				if err := SaveOrchestrationPlan(plan); err != nil {
					logger.LogError(fmt.Errorf("failed to save final orchestration plan after max validation attempts: %w", err))
				}
				os.Exit(1) // Exit with error code
			}

			if err := SaveOrchestrationPlan(plan); err != nil {
				logger.LogError(fmt.Errorf("failed to save orchestration plan after validation attempt: %w", err))
			}
		}

		if validationSuccess {
			plan.Status = "completed"
			logger.LogProcessStep("\nAll orchestration requirements and final validation completed successfully!")
		} else {
			// This else block should only be reached if max attempts were reached and os.Exit(1) was not called
			// (e.g., if there was an error saving the plan after max attempts).
			// In normal flow, os.Exit(1) would have been called.
			plan.Status = "failed"
			logger.LogProcessStep("\nOrchestration failed after multiple validation attempts. Please review the plan.")
		}

	} else {
		plan.Status = "in_progress" // Or "partially_completed"
		logger.LogProcessStep("\nOrchestration completed with pending or failed requirements. Please review the plan.")
	}

	if err := SaveOrchestrationPlan(plan); err != nil { // Use SaveOrchestrationPlan from current package
		logger.LogError(fmt.Errorf("failed to save final orchestration plan: %w", err))
	}

	return nil
}

// hasPendingRequirements checks if there are any pending requirements in the plan.
func hasPendingRequirements(plan *types.OrchestrationPlan) bool {
	for _, req := range plan.Requirements {
		if req.Status != "completed" {
			return true
		}
	}
	return false
}

// generateRequirements asks the LLM to generate a high-level orchestration plan.
func generateRequirements(prompt string, cfg *config.Config) (*types.OrchestrationPlan, error) {
	logger := utils.GetLogger(cfg.SkipPrompt)
	modelName := cfg.OrchestrationModel
	if modelName == "" {
		modelName = cfg.EditingModel // Fallback
	}
	logger.LogProcessStep(fmt.Sprintf("Using model %s for orchestration planning.", modelName))

	workspaceContext := workspace.GetWorkspaceContext(prompt, cfg)
	messages := prompts.BuildOrchestrationPlanMessages(prompt, workspaceContext)

	// Use a longer timeout for this, as it's a planning step
	_, response, err := llm.GetLLMResponse(modelName, messages, "", cfg, 3*time.Minute, false) // No search grounding for this planning step
	if err != nil {
		return nil, fmt.Errorf("failed to get orchestration plan from LLM: %w", err)
	}

	if response == "" {
		return nil, fmt.Errorf("LLM returned an empty response for orchestration plan")
	}

	// Try to extract JSON from response (handles both raw JSON and code block JSON)
	var jsonStr string
	if strings.Contains(response, "```json") {
		// Handle code block JSON
		parts := strings.Split(response, "```json")
		if len(parts) > 1 {
			jsonPart := parts[1]
			end := strings.Index(jsonPart, "```")
			if end > 0 {
				jsonStr = strings.TrimSpace(jsonPart[:end])
			} else {
				jsonStr = strings.TrimSpace(jsonPart)
			}
		}
	} else if strings.Contains(response, `"requirements"`) { // Heuristic to detect raw JSON
		jsonStr = response
	}

	if jsonStr == "" {
		return nil, fmt.Errorf("LLM response did not contain expected JSON for orchestration plan: %s", response)
	}

	var plan types.OrchestrationPlan
	if err := json.Unmarshal([]byte(jsonStr), &plan); err != nil {
		return nil, fmt.Errorf("failed to parse orchestration plan JSON from LLM response: %w\nResponse was: %s", err, response)
	}

	// Initialize requirement statuses and IDs
	for i := range plan.Requirements {
		plan.Requirements[i].ID = fmt.Sprintf("req-%d", i+1)
		if plan.Requirements[i].Status == "" {
			plan.Requirements[i].Status = "pending"
		}
	}
	plan.CurrentStep = 0
	plan.Status = "pending"

	return &plan, nil
}

func validateChanges() error {
	// load in workspace json file
	workspaceFile, err := workspace.LoadWorkspaceFile()
	if err != nil {
		return fmt.Errorf("failed to load workspace file: %w", err)
	}
	build := workspaceFile.BuildCommand
	if build == "" {
		return fmt.Errorf("no build script found in workspace file")
	}

	cmd := exec.Command("sh", "-c", build)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Run() // Run the build script to ensure it works
	if err != nil {
		return fmt.Errorf("build command failed: %w\nOutput:\n%s", err, stderr.String())
	}

	return nil
}
