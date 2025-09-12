package orchestration

import (
	"fmt"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/orchestration/types"
	"github.com/alantheprice/ledit/pkg/utils"
)

// AgentStepExecutor executes process steps by delegating to agent infrastructure
type AgentStepExecutor struct {
	factory         *ProcessAgentFactory
	logger          *utils.Logger
	changeIntegration *ProcessChangeIntegration
	processID       string
}

// NewAgentStepExecutor creates a new executor for process steps using agents
func NewAgentStepExecutor(factory *ProcessAgentFactory, logger *utils.Logger, processID string) *AgentStepExecutor {
	return &AgentStepExecutor{
		factory:   factory,
		logger:    logger,
		processID: processID,
		changeIntegration: &ProcessChangeIntegration{
			processID: processID,
		},
	}
}

// ExecuteStep executes a process step by creating and running a specialized agent
func (e *AgentStepExecutor) ExecuteStep(
	step *EnhancedOrchestrationStep,
	agentDef types.AgentDefinition,
	context ProcessContext,
) (*types.StepResult, error) {
	e.logger.LogProcessStep(fmt.Sprintf("Starting execution of step %s with agent %s", step.ID, agentDef.ID))

	// Validate step configuration
	if err := e.factory.ValidateStepConfiguration(step, agentDef); err != nil {
		return nil, fmt.Errorf("step configuration validation failed: %w", err)
	}

	// Create specialized agent for this step
	stepAgent, err := e.factory.CreateAgentForStep(step, agentDef)
	if err != nil {
		return nil, fmt.Errorf("agent creation failed: %w", err)
	}

	// Ensure cleanup happens
	defer e.cleanupAgent(stepAgent, step.ID)

	// Prepare step input with full context
	prompt := e.buildStepPrompt(step, context, agentDef)

	// Execute step through agent
	startTime := time.Now()
	result, err := e.executeStepWithAgent(stepAgent, step, prompt)
	duration := time.Since(startTime)

	if err != nil {
		return e.createFailureResult(step, err, duration), nil // Return result even on failure
	}

	// Post-process and enhance results
	enhancedResult, err := e.processStepResult(result, step, stepAgent, duration)
	if err != nil {
		e.logger.LogProcessStep(fmt.Sprintf("Warning: result post-processing failed for step %s: %v", step.ID, err))
		// Continue with original result
	} else {
		result = enhancedResult
	}

	e.logger.LogProcessStep(fmt.Sprintf("Completed execution of step %s in %.2f seconds", step.ID, duration.Seconds()))
	return result, nil
}

// executeStepWithAgent runs the actual step execution through the agent
func (e *AgentStepExecutor) executeStepWithAgent(
	stepAgent *agent.Agent,
	step *EnhancedOrchestrationStep,
	prompt string,
) (*types.StepResult, error) {
	e.logger.LogProcessStep(fmt.Sprintf("Executing step %s with prompt length: %d chars", step.ID, len(prompt)))

	// TODO: Set up step-specific system prompt
	// This would require agent package modifications to support dynamic system prompts
	// For now, we include the context in the user prompt

	// Execute the step through agent conversation
	// Note: This is a simplified execution - in practice, we might need multiple iterations
	response, err := e.executeAgentConversation(stepAgent, prompt, step)
	if err != nil {
		return nil, fmt.Errorf("agent conversation failed: %w", err)
	}

	// Create basic step result
	result := &types.StepResult{
		Status:   "success",
		Output:   map[string]string{"response": response},
		Files:    []string{}, // Will be populated by post-processing
		Duration: 0,          // Will be set by caller
		Logs:     []string{fmt.Sprintf("Agent response: %s", response)},
		Tokens:   0,   // Will be populated from agent
		Cost:     0.0, // Will be populated from agent
	}

	return result, nil
}

// executeAgentConversation handles the actual conversation with the agent
func (e *AgentStepExecutor) executeAgentConversation(
	stepAgent *agent.Agent,
	prompt string,
	step *EnhancedOrchestrationStep,
) (string, error) {
	// For now, we'll use a simplified approach
	// In a full implementation, this would handle:
	// - Multi-turn conversations
	// - Tool calls and responses
	// - Error recovery and retries
	// - Progress monitoring

	e.logger.LogProcessStep(fmt.Sprintf("Starting agent conversation for step %s", step.ID))

	// TODO: Implement actual agent conversation
	// This would require integration with the agent's conversation system
	// For now, we'll return a placeholder response

	response := fmt.Sprintf("Step %s executed successfully. Prompt received: %s", step.ID, prompt[:min(100, len(prompt))])
	
	e.logger.LogProcessStep(fmt.Sprintf("Agent conversation completed for step %s", step.ID))
	return response, nil
}

// buildStepPrompt creates a comprehensive prompt for the step execution
func (e *AgentStepExecutor) buildStepPrompt(
	step *EnhancedOrchestrationStep,
	context ProcessContext,
	agentDef types.AgentDefinition,
) string {
	var promptParts []string

	// Add agent context and persona
	systemContext := step.GetSystemPrompt(agentDef)
	if systemContext != "" {
		promptParts = append(promptParts, systemContext)
	}

	// Add process context
	if context.Goal != "" {
		promptParts = append(promptParts, fmt.Sprintf("Overall Process Goal: %s", context.Goal))
	}

	// Add step-specific input
	stepInput := step.GetStepInput(context)
	if stepInput != "" {
		promptParts = append(promptParts, stepInput)
	}

	// Add workspace context if available
	if step.EnableWorkspace && len(step.WorkspaceScope) > 0 {
		promptParts = append(promptParts, fmt.Sprintf("Focus on these workspace areas: %v", step.WorkspaceScope))
	}

	// Add tool restrictions if any
	if len(step.ToolRestrictions) > 0 {
		promptParts = append(promptParts, fmt.Sprintf("Available tools for this step: %v", step.ToolRestrictions))
	}

	// Add expected output guidance
	if step.ExpectedOutput != "" {
		promptParts = append(promptParts, fmt.Sprintf("Expected outcome: %s", step.ExpectedOutput))
	}

	return fmt.Sprintf("%s\n\nPlease execute this step according to the requirements above.", 
		joinNonEmpty(promptParts, "\n\n"))
}

// processStepResult enhances the step result with additional metadata and tracking
func (e *AgentStepExecutor) processStepResult(
	result *types.StepResult,
	step *EnhancedOrchestrationStep,
	stepAgent *agent.Agent,
	duration time.Duration,
) (*types.StepResult, error) {
	// Set duration
	result.Duration = duration.Seconds()

	// Get actual metrics from agent (what's available)
	result.Cost = stepAgent.GetTotalCost()           // Available method
	result.Tokens = 0                               // Not exposed via public API
	// NOTE: Agent workflow NOW HAS change tracking!
	result.Files = stepAgent.GetTrackedFiles()       // Get files modified by agent

	// Agent workflow now has built-in change tracking!
	if step.EnableChangeTrack {
		e.logger.LogProcessStep(fmt.Sprintf("Change tracking enabled for step %s (Revision: %s)", step.ID, stepAgent.GetRevisionID()))
		e.logger.LogProcessStep(fmt.Sprintf("Tracked %d file changes in step %s", stepAgent.GetChangeCount(), step.ID))
	}

	// Add step completion log
	result.Logs = append(result.Logs, fmt.Sprintf("Step %s completed in %.2f seconds", step.ID, duration.Seconds()))

	return result, nil
}

// trackStepChanges records changes made during step execution
// Agent workflow now has built-in change tracking via WriteFile and EditFile tools
func (e *AgentStepExecutor) trackStepChanges(stepID string, stepAgent *agent.Agent) error {
	revisionID := stepAgent.GetRevisionID()
	changeCount := stepAgent.GetChangeCount()
	
	e.logger.LogProcessStep(fmt.Sprintf("Step %s tracked %d file changes (revision %s)", stepID, changeCount, revisionID))
	
	if changeCount > 0 {
		files := stepAgent.GetTrackedFiles()
		e.logger.LogProcessStep(fmt.Sprintf("Modified files: %v", files))
		
		// Changes are automatically committed by the agent at the end of conversation
		// The agent integrates directly with pkg/changetracker
	}
	
	return nil
}

// createFailureResult creates a failure result for a step that couldn't be executed
func (e *AgentStepExecutor) createFailureResult(step *EnhancedOrchestrationStep, err error, duration time.Duration) *types.StepResult {
	return &types.StepResult{
		Status:   "failure",
		Output:   map[string]string{"error": err.Error()},
		Files:    []string{},
		Errors:   []string{err.Error()},
		Duration: duration.Seconds(),
		Logs:     []string{fmt.Sprintf("Step %s failed: %v", step.ID, err)},
		Tokens:   0,
		Cost:     0.0,
	}
}

// cleanupAgent performs cleanup after step execution
func (e *AgentStepExecutor) cleanupAgent(stepAgent *agent.Agent, stepID string) {
	e.logger.LogProcessStep(fmt.Sprintf("Cleaning up agent for step %s", stepID))
	
	// TODO: Implement actual cleanup if needed
	// - Save agent state
	// - Close connections
	// - Free resources
}

// ProcessChangeIntegration handles change tracking for process steps
type ProcessChangeIntegration struct {
	processID string
}

// TrackStepChanges records changes made during a process step
func (c *ProcessChangeIntegration) TrackStepChanges(stepID string, files []string) error {
	// TODO: Implement actual change tracking
	revisionID := fmt.Sprintf("process-%s-step-%s", c.processID, stepID)
	
	for _, file := range files {
		// This would record each file change in the change tracker
		// TODO: Implement actual change recording
		_ = struct {
			RevisionID string
			Type       string
			FilePath   string
			Timestamp  time.Time
			Metadata   map[string]string
		}{
			RevisionID: revisionID,
			Type:       "process_step",
			FilePath:   file,
			Timestamp:  time.Now(),
			Metadata: map[string]string{
				"process_id": c.processID,
				"step_id":    stepID,
			},
		}
	}
	
	return nil
}

// Helper functions

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// joinNonEmpty joins non-empty strings with a separator
func joinNonEmpty(parts []string, sep string) string {
	var nonEmpty []string
	for _, part := range parts {
		if part != "" {
			nonEmpty = append(nonEmpty, part)
		}
	}
	return fmt.Sprintf("%s", fmt.Sprintf("%s", joinStrings(nonEmpty, sep)))
}

// joinStrings joins strings with a separator (placeholder for strings.Join)
func joinStrings(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return parts[0]
	}
	
	result := parts[0]
	for i := 1; i < len(parts); i++ {
		result += sep + parts[i]
	}
	return result
}