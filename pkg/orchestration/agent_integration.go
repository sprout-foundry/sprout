package orchestration

import (
	"fmt"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/orchestration/types"
	"github.com/alantheprice/ledit/pkg/utils"
)

// AgentIntegratedOrchestrator extends the existing orchestrator with agent delegation
type AgentIntegratedOrchestrator struct {
	*MultiAgentOrchestrator // Embed existing orchestrator
	agentFactory            *ProcessAgentFactory
	agentExecutor           *AgentStepExecutor
	enableAgentDelegation   bool
}

// NewAgentIntegratedOrchestrator creates an orchestrator that delegates to agent infrastructure
func NewAgentIntegratedOrchestrator(
	processFile *types.ProcessFile,
	cfg *config.Config,
	logger *utils.Logger,
	resume bool,
	statePath string,
	processID string,
) *AgentIntegratedOrchestrator {
	// Create base orchestrator
	baseOrchestrator := NewMultiAgentOrchestrator(processFile, cfg, logger, resume, statePath)

	// Create agent factory and executor for delegation
	agentFactory := NewProcessAgentFactory(cfg, logger, processID)
	agentExecutor := NewAgentStepExecutor(agentFactory, logger, processID)

	return &AgentIntegratedOrchestrator{
		MultiAgentOrchestrator: baseOrchestrator,
		agentFactory:           agentFactory,
		agentExecutor:          agentExecutor,
		enableAgentDelegation:  true, // Enable by default
	}
}

// ExecuteStepWithAgent executes a step using agent delegation instead of legacy approach
func (o *AgentIntegratedOrchestrator) ExecuteStepWithAgent(
	step types.OrchestrationStep,
	agentDef types.AgentDefinition,
) (*types.StepResult, error) {
	o.logger.LogProcessStep(fmt.Sprintf("Executing step %s with agent delegation", step.ID))

	// Convert to enhanced step
	enhancedStep := ToEnhancedStep(&step)

	// Create process context
	context := o.buildProcessContext(step)

	// Execute through agent infrastructure
	result, err := o.agentExecutor.ExecuteStep(enhancedStep, agentDef, context)
	if err != nil {
		return nil, fmt.Errorf("agent delegation failed for step %s: %w", step.ID, err)
	}

	o.logger.LogProcessStep(fmt.Sprintf("Step %s completed via agent delegation", step.ID))
	return result, nil
}

// buildProcessContext creates context for step execution
func (o *AgentIntegratedOrchestrator) buildProcessContext(step types.OrchestrationStep) ProcessContext {
	// Get previous step outputs
	previousOutput := make(map[string]string)
	for _, prevStep := range o.plan.Steps {
		if prevStep.Status == "completed" && prevStep.Result != nil {
			for key, value := range prevStep.Result.Output {
				previousOutput[fmt.Sprintf("%s_%s", prevStep.ID, key)] = value
			}
		}
	}

	return ProcessContext{
		ProcessID:      o.plan.Goal, // Use goal as process identifier
		Goal:           o.plan.Goal,
		CurrentStep:    o.plan.CurrentStep,
		TotalSteps:     len(o.plan.Steps),
		PreviousOutput: previousOutput,
		ProcessState:   make(map[string]interface{}), // Could be populated with shared state
		WorkspaceInfo:  nil,                          // Could be populated with workspace analysis
	}
}

// ShouldUseAgentDelegation determines if a step should use agent delegation
func (o *AgentIntegratedOrchestrator) ShouldUseAgentDelegation(step types.OrchestrationStep) bool {
	if !o.enableAgentDelegation {
		return false
	}

	// Use agent delegation for steps that would benefit from:
	// 1. Code generation/modification
	// 2. Complex analysis tasks
	// 3. Multi-tool workflows

	// For now, enable for all steps - can be made configurable later
	return true
}

// EnableAgentDelegation allows toggling agent delegation on/off
func (o *AgentIntegratedOrchestrator) EnableAgentDelegation(enable bool) {
	o.enableAgentDelegation = enable
	if enable {
		o.logger.LogProcessStep("Agent delegation enabled for process steps")
	} else {
		o.logger.LogProcessStep("Agent delegation disabled - using legacy execution")
	}
}

// GetAgentFactory returns the agent factory for external use
func (o *AgentIntegratedOrchestrator) GetAgentFactory() *ProcessAgentFactory {
	return o.agentFactory
}

// GetAgentExecutor returns the agent executor for external use  
func (o *AgentIntegratedOrchestrator) GetAgentExecutor() *AgentStepExecutor {
	return o.agentExecutor
}

// ValidateAgentIntegration validates that agent integration is properly configured
func (o *AgentIntegratedOrchestrator) ValidateAgentIntegration() error {
	if o.agentFactory == nil {
		return fmt.Errorf("agent factory not initialized")
	}
	if o.agentExecutor == nil {
		return fmt.Errorf("agent executor not initialized")
	}

	o.logger.LogProcessStep("Agent integration validation successful")
	return nil
}

// GetIntegrationStatus returns the status of agent integration
func (o *AgentIntegratedOrchestrator) GetIntegrationStatus() map[string]interface{} {
	return map[string]interface{}{
		"agent_delegation_enabled": o.enableAgentDelegation,
		"agent_factory_available":  o.agentFactory != nil,
		"agent_executor_available": o.agentExecutor != nil,
		"integration_ready":        o.ValidateAgentIntegration() == nil,
	}
}

// ProcessStepWithIntelligence executes a step with enhanced workspace intelligence
func (o *AgentIntegratedOrchestrator) ProcessStepWithIntelligence(
	step types.OrchestrationStep,
	agentDef types.AgentDefinition,
	enableWorkspace bool,
	enableChangeTracking bool,
) (*types.StepResult, error) {
	// Convert to enhanced step with intelligence features
	enhancedStep := ToEnhancedStep(&step)
	enhancedStep.EnableWorkspace = enableWorkspace
	enhancedStep.EnableChangeTrack = enableChangeTracking

	// Configure agent for enhanced capabilities
	if enhancedStep.AgentConfig == nil {
		enhancedStep.AgentConfig = &AgentStepConfig{}
	}
	enhancedStep.AgentConfig.EnableWorkspace = enableWorkspace
	enhancedStep.AgentConfig.EnableChangeTrack = enableChangeTracking

	// Create context with intelligence features
	context := o.buildProcessContext(step)

	// Execute with full agent capabilities
	return o.agentExecutor.ExecuteStep(enhancedStep, agentDef, context)
}