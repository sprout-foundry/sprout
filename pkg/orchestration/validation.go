package orchestration

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/orchestration/types"
	ui "github.com/alantheprice/ledit/pkg/ui"
)

// validateResults performs final validation of orchestration results
func (o *MultiAgentOrchestrator) validateResults() error {
	if o.validation == nil {
		o.logger.LogProcessStep("✅ No validation rules specified - results validation skipped")
		return nil
	}

	o.logger.LogProcessStep("🔍 Validating orchestration results...")

	// Check if all steps completed successfully
	failedSteps := 0
	for _, step := range o.plan.Steps {
		if step.Status == "failed" {
			failedSteps++
		}
	}

	if failedSteps > 0 {
		return fmt.Errorf("validation failed: %d steps did not complete successfully", failedSteps)
	}

	o.logger.LogProcessStep("✅ Results validation completed successfully")
	return nil
}

// Note: File validation functions removed as RequiredFiles/ForbiddenFiles fields don't exist in ValidationConfig

// runValidationStage executes configured validation commands
func (o *MultiAgentOrchestrator) runValidationStage() error {
	if o.validation == nil {
		return nil
	}

	o.logger.LogProcessStep("🧪 Running validation commands...")

	// Run build command
	if o.validation.BuildCommand != "" {
		if err := o.runValidationCommand(o.validation.BuildCommand, "build"); err != nil {
			return fmt.Errorf("build validation failed: %w", err)
		}
	}

	// Run test command
	if o.validation.TestCommand != "" {
		if err := o.runValidationCommand(o.validation.TestCommand, "test"); err != nil {
			return fmt.Errorf("test validation failed: %w", err)
		}
	}

	// Run lint command
	if o.validation.LintCommand != "" {
		if err := o.runValidationCommand(o.validation.LintCommand, "lint"); err != nil {
			return fmt.Errorf("lint validation failed: %w", err)
		}
	}

	// Run custom checks
	for i, cmd := range o.validation.CustomChecks {
		if err := o.runValidationCommand(cmd, fmt.Sprintf("custom_check_%d", i+1)); err != nil {
			return fmt.Errorf("custom check %d failed: %w", i+1, err)
		}
	}

	o.logger.LogProcessStep("✅ Validation commands completed")
	return nil
}

// runValidationCommand executes a single validation command
func (o *MultiAgentOrchestrator) runValidationCommand(cmdStr, name string) error {
	o.logger.LogProcessStep(fmt.Sprintf("▶️ Running %s validation: %s", name, cmdStr))

	// Prepare command execution
	command := exec.Command("sh", "-c", cmdStr)

	// Execute command
	output, err := command.CombinedOutput()

	// Log output
	if len(output) > 0 {
		o.logger.LogProcessStep(fmt.Sprintf("📄 Validation output: %s", string(output)))
	}

	if err != nil {
		return fmt.Errorf("validation command '%s' failed: %w", name, err)
	}

	o.logger.LogProcessStep(fmt.Sprintf("✅ Validation passed: %s", name))
	return nil
}

// checkAgentBudget validates an agent's resource usage against limits
func (o *MultiAgentOrchestrator) checkAgentBudget(agentRunner *AgentRunner) error {
	if agentRunner.definition.Budget == nil {
		return nil // No budget constraints
	}

	budget := agentRunner.definition.Budget
	status := o.plan.AgentStatuses[agentRunner.definition.ID]

	// Check token usage
	if budget.MaxTokens > 0 && status.TokenUsage >= budget.MaxTokens {
		return fmt.Errorf("agent %s has exceeded token budget (%d/%d)",
			agentRunner.definition.Name, status.TokenUsage, budget.MaxTokens)
	}

	// Check cost
	if budget.MaxCost > 0 && status.Cost >= budget.MaxCost {
		return fmt.Errorf("agent %s has exceeded cost budget ($%.4f/$%.4f)",
			agentRunner.definition.Name, status.Cost, budget.MaxCost)
	}

	// Warn about approaching limits
	if budget.MaxTokens > 0 && float64(status.TokenUsage) > float64(budget.MaxTokens)*0.9 {
		o.logger.LogProcessStep(fmt.Sprintf("⚠️ Agent '%s' approaching token limit: %d/%d",
			agentRunner.definition.Name, status.TokenUsage, budget.MaxTokens))
	}

	if budget.MaxCost > 0 && status.Cost > budget.MaxCost*0.9 {
		o.logger.LogProcessStep(fmt.Sprintf("⚠️ Agent '%s' approaching cost limit: $%.4f/$%.4f",
			agentRunner.definition.Name, status.Cost, budget.MaxCost))
	}

	return nil
}

// updateAgentBudget updates an agent's resource usage after task completion
func (o *MultiAgentOrchestrator) updateAgentBudget(agentRunner *AgentRunner, tokenUsage *types.AgentTokenUsage) {
	if tokenUsage == nil || agentRunner.definition.Budget == nil {
		return
	}

	status := o.plan.AgentStatuses[agentRunner.definition.ID]
	budget := agentRunner.definition.Budget

	// Update token usage
	status.TokenUsage += tokenUsage.Total

	// Calculate cost if possible
	if agentRunner.config != nil && agentRunner.config.EditingModel != "" {
		tokenCost := llm.CalculateCost(llm.TokenUsage{
			PromptTokens:     tokenUsage.Prompt,
			CompletionTokens: tokenUsage.Completion,
			TotalTokens:      tokenUsage.Total,
		}, agentRunner.config.EditingModel)
		status.Cost += tokenCost
	}

	// Enforce hard limits
	if (budget.MaxTokens > 0 && status.TokenUsage > budget.MaxTokens) || (budget.MaxCost > 0 && status.Cost > budget.MaxCost) {
		status.Halted = true
		status.HaltReason = fmt.Sprintf("budget exceeded (tokens %d/%d, cost $%.4f/$%.4f)",
			status.TokenUsage, budget.MaxTokens, status.Cost, budget.MaxCost)
		if budget.StopOnLimit {
			o.logger.LogProcessStep(fmt.Sprintf("🛑 Agent '%s' halted: %s", agentRunner.definition.Name, status.HaltReason))
		} else {
			o.logger.LogProcessStep(fmt.Sprintf("⚠️ Agent '%s' exceeded budget but continuing: %s", agentRunner.definition.Name, status.HaltReason))
		}
	}

	// Log budget status
	o.logger.LogProcessStep(fmt.Sprintf("💰 Agent '%s' budget status: %d tokens, $%.4f cost",
		agentRunner.definition.Name, status.TokenUsage, status.Cost))

	// Update the status in the map
	o.plan.AgentStatuses[agentRunner.definition.ID] = status
}

// enrichStepWithToolContext adds tool-assisted context to a step
func (o *MultiAgentOrchestrator) enrichStepWithToolContext(step *types.OrchestrationStep) {
	// Collect files changed since a reasonable time window
	since := time.Now().Add(-time.Hour) // Last hour
	changedFiles := o.collectChangedFilesSince(since)

	if len(changedFiles) > 0 {
		context := "\nRecent file changes (last hour):\n"
		for _, file := range changedFiles {
			context += fmt.Sprintf("  • %s\n", file)
		}
		step.Input["file_changes"] = context
	}

	// Add any tool-assisted insights
	// Convert input map to string for tool assistance
	inputStr := ""
	for key, value := range step.Input {
		inputStr += fmt.Sprintf("%s: %s\n", key, value)
	}
	toolContext := o.toolAssistTask(inputStr)
	if toolContext != "" {
		step.Input["tool_context"] = toolContext
	}
}

// collectChangedFilesSince returns files that have been modified since the given time
func (o *MultiAgentOrchestrator) collectChangedFilesSince(since time.Time) []string {
	var changedFiles []string

	// This is a simple implementation - in a real system, you might want to:
	// 1. Use git log to find changed files
	// 2. Use filesystem monitoring
	// 3. Track changes during orchestration

	// For now, just return an empty list
	// TODO: Implement actual file change detection
	return changedFiles
}

// toolAssistTask uses available tools to enhance task understanding
func (o *MultiAgentOrchestrator) toolAssistTask(task string) string {
	// This is a placeholder for tool-assisted task enhancement
	// In a real implementation, this could:
	// 1. Analyze code dependencies
	// 2. Check for related files
	// 3. Provide context about the codebase
	// 4. Suggest implementation approaches

	// For now, return empty string
	return ""
}

// validateAgentConfiguration validates an agent's configuration
func (o *MultiAgentOrchestrator) validateAgentConfiguration(agentDef *types.AgentDefinition) error {
	if agentDef.ID == "" {
		return fmt.Errorf("agent ID is required")
	}

	if agentDef.Name == "" {
		return fmt.Errorf("agent name is required")
	}

	if agentDef.Persona == "" {
		return fmt.Errorf("agent persona is required")
	}

	// Validate model if specified
	if agentDef.Model != "" {
		validModels := []string{"gpt-4", "gpt-3.5-turbo", "claude-3", "gemini-pro"} // Add more as needed
		isValid := false
		for _, model := range validModels {
			if strings.Contains(agentDef.Model, model) {
				isValid = true
				break
			}
		}
		if !isValid {
			o.logger.LogProcessStep(fmt.Sprintf("⚠️ Unknown model '%s' for agent '%s'", agentDef.Model, agentDef.Name))
		}
	}

	// Validate budget if specified
	if agentDef.Budget != nil {
		if agentDef.Budget.MaxTokens < 0 {
			return fmt.Errorf("agent %s has negative max tokens", agentDef.ID)
		}
		if agentDef.Budget.MaxCost < 0 {
			return fmt.Errorf("agent %s has negative max cost", agentDef.ID)
		}
	}

	return nil
}

// validateStepConfiguration validates a step's configuration
func (o *MultiAgentOrchestrator) validateStepConfiguration(step *types.OrchestrationStep) error {
	if step.ID == "" {
		return fmt.Errorf("step ID is required")
	}

	if step.Name == "" {
		return fmt.Errorf("step name is required")
	}

	if step.AgentID == "" {
		return fmt.Errorf("step agent ID is required")
	}

	// Check if the assigned agent exists
	agentExists := false
	for _, agent := range o.plan.Agents {
		if agent.ID == step.AgentID {
			agentExists = true
			break
		}
	}
	if !agentExists {
		return fmt.Errorf("step %s references unknown agent %s", step.ID, step.AgentID)
	}

	// Validate timeout
	if step.Timeout < 0 {
		return fmt.Errorf("step %s has negative timeout", step.ID)
	}

	// Validate retries
	if step.Retries < 0 {
		return fmt.Errorf("step %s has negative retries", step.ID)
	}

	return nil
}

// printProgressTable displays a formatted progress table
func (o *MultiAgentOrchestrator) printProgressTable() {
	if ui.IsUIActive() {
		ui.Out().Print("\n📊 Orchestration Progress\n")
		ui.Out().Print("═══════════════════════════════════════\n")

		for _, step := range o.plan.Steps {
			var statusIcon string
			switch step.Status {
			case "completed":
				statusIcon = "✅"
			case "in_progress":
				statusIcon = "🔄"
			case "failed":
				statusIcon = "❌"
			default:
				statusIcon = "⏳"
			}

			agentName := o.getAgentName(step.AgentID)
			ui.Out().Printf("%s %-20s | %-12s | %s\n",
				statusIcon, step.Name, agentName, step.Status)
		}
		ui.Out().Print("═══════════════════════════════════════\n")
	}
}

// Helper functions for progress display
func (o *MultiAgentOrchestrator) getAgentName(agentID string) string {
	for _, agent := range o.plan.Agents {
		if agent.ID == agentID {
			return agent.Name
		}
	}
	return agentID
}

func (o *MultiAgentOrchestrator) getAgentDefinition(agentID string) *types.AgentDefinition {
	for _, agent := range o.plan.Agents {
		if agent.ID == agentID {
			return &agent
		}
	}
	return nil
}

func (o *MultiAgentOrchestrator) updateAgentStatus(agentID, status, currentStep string, progress int) {
	if agentStatus, exists := o.plan.AgentStatuses[agentID]; exists {
		agentStatus.Status = status
		agentStatus.CurrentStep = currentStep
		agentStatus.Progress = progress
		agentStatus.LastUpdate = time.Now().Format(time.RFC3339)
		o.plan.AgentStatuses[agentID] = agentStatus
	}
}
