package orchestration

import (
	"fmt"

	"github.com/alantheprice/ledit/pkg/agent"
	agent_config "github.com/alantheprice/ledit/pkg/agent_config"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/orchestration/types"
	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/alantheprice/ledit/pkg/workspace"
)

// ProcessAgentFactory creates and configures agents for process step execution
type ProcessAgentFactory struct {
	workspaceAnalyzer *workspace.ConcurrentAnalyzer
	configManager     *agent_config.Manager
	defaultConfig     *config.Config
	logger            *utils.Logger
	processID         string
}

// NewProcessAgentFactory creates a new factory for process step agents
func NewProcessAgentFactory(cfg *config.Config, logger *utils.Logger, processID string) *ProcessAgentFactory {
	// Create workspace analyzer for intelligent context building
	workspaceAnalyzer := workspace.NewConcurrentAnalyzer(workspace.ConcurrentConfig{
		MaxWorkers: 4,
		BatchSize:  10,
	})

	// Create agent config manager
	configManager, err := agent_config.NewManager()
	if err != nil {
		logger.LogError(fmt.Errorf("failed to create agent config manager: %w", err))
		configManager = nil
	}

	return &ProcessAgentFactory{
		workspaceAnalyzer: workspaceAnalyzer,
		configManager:     configManager,
		defaultConfig:     cfg,
		logger:            logger,
		processID:         processID,
	}
}

// CreateAgentForStep creates a specialized agent for executing a process step
func (f *ProcessAgentFactory) CreateAgentForStep(
	step *EnhancedOrchestrationStep,
	agentDef types.AgentDefinition,
) (*agent.Agent, error) {
	f.logger.LogProcessStep(fmt.Sprintf("Creating agent for step %s with persona %s", step.ID, agentDef.Persona))

	// Determine model (step override > agent override > default)
	model := step.GetModel(agentDef, f.defaultConfig.EditingModel)
	if model == "" {
		model = "gpt-4o-mini" // Final fallback
	}

	// Create agent with step-specific model
	stepAgent, err := agent.NewAgentWithModel(model)
	if err != nil {
		return nil, fmt.Errorf("failed to create agent for step %s: %w", step.ID, err)
	}

	// Configure agent for this specific step
	if err := f.configureAgentForStep(stepAgent, step, agentDef); err != nil {
		return nil, fmt.Errorf("failed to configure agent for step %s: %w", step.ID, err)
	}

	f.logger.LogProcessStep(fmt.Sprintf("Successfully created and configured agent for step %s", step.ID))
	return stepAgent, nil
}

// configureAgentForStep applies step-specific configuration to the agent
func (f *ProcessAgentFactory) configureAgentForStep(
	stepAgent *agent.Agent,
	step *EnhancedOrchestrationStep,
	agentDef types.AgentDefinition,
) error {
	// Set up workspace context if enabled
	if step.EnableWorkspace && step.AgentConfig != nil && step.AgentConfig.EnableWorkspace {
		if err := f.enhanceAgentWithWorkspace(stepAgent, step); err != nil {
			f.logger.LogProcessStep(fmt.Sprintf("Warning: workspace enhancement failed for step %s: %v", step.ID, err))
			// Don't fail the whole step - workspace enhancement is nice-to-have
		}
	}

	// Set up change tracking if enabled
	if step.EnableChangeTrack && step.AgentConfig != nil && step.AgentConfig.EnableChangeTrack {
		if err := f.enableChangeTracking(stepAgent, step.ID); err != nil {
			f.logger.LogProcessStep(fmt.Sprintf("Warning: change tracking setup failed for step %s: %v", step.ID, err))
			// Don't fail the whole step - change tracking is nice-to-have
		}
	}

	// Apply tool restrictions if specified
	if len(step.ToolRestrictions) > 0 {
		if err := f.applyToolRestrictions(stepAgent, step.ToolRestrictions); err != nil {
			f.logger.LogProcessStep(fmt.Sprintf("Warning: tool restriction setup failed for step %s: %v", step.ID, err))
			// Don't fail the whole step - proceed with all tools available
		}
	}

	// Set step-specific system prompt combining agent persona + step context
	systemPrompt := step.GetSystemPrompt(agentDef)
	f.logger.LogProcessStep(fmt.Sprintf("Setting system prompt for step %s (length: %d chars)", step.ID, len(systemPrompt)))

	// Note: We'll apply the system prompt when the agent starts conversation
	// For now, we store it for later use

	return nil
}

// enhanceAgentWithWorkspace adds workspace intelligence to the agent
func (f *ProcessAgentFactory) enhanceAgentWithWorkspace(
	stepAgent *agent.Agent,
	step *EnhancedOrchestrationStep,
) error {
	if f.workspaceAnalyzer == nil {
		return fmt.Errorf("workspace analyzer not available")
	}

	f.logger.LogProcessStep(fmt.Sprintf("Enhancing agent with workspace intelligence for step %s", step.ID))

	// Get workspace information (disabled for now due to integration complexity)
	// TODO: Implement workspace analyzer integration when workspace package is stable
	// workspaceInfo, err := f.workspaceAnalyzer.GetWorkspaceInfo()
	// if err != nil {
	//     return fmt.Errorf("workspace analysis failed: %w", err)
	// }

	// For now, we'll prepare the agent to receive workspace context when it starts
	f.logger.LogProcessStep("Workspace intelligence enhancement prepared for step execution")

	return nil
}

// enableChangeTracking enables change tracking for the agent
func (f *ProcessAgentFactory) enableChangeTracking(
	stepAgent *agent.Agent,
	stepID string,
) error {
	f.logger.LogProcessStep(fmt.Sprintf("Enabling change tracking for step %s", stepID))

	// Agent workflow now has built-in change tracking!
	// Change tracking is automatically enabled when the agent processes a query
	// The agent will track WriteFile and EditFile operations
	revisionID := stepAgent.GetRevisionID()
	f.logger.LogProcessStep(fmt.Sprintf("Change tracking enabled for step %s (revision: %s)", stepID, revisionID))

	return nil
}

// applyToolRestrictions limits the tools available to the agent
func (f *ProcessAgentFactory) applyToolRestrictions(
	stepAgent *agent.Agent,
	allowedTools []string,
) error {
	f.logger.LogProcessStep(fmt.Sprintf("Applying tool restrictions: %v", allowedTools))

	// TODO: Implement tool restriction mechanism in agent
	// This would require agent package modifications to support tool filtering
	// For now, we just log the intended restrictions
	f.logger.LogProcessStep("Tool restrictions logged (implementation pending)")

	return nil
}

// GetRevisionIDForStep generates a revision ID for change tracking
func (f *ProcessAgentFactory) GetRevisionIDForStep(stepID string) string {
	return fmt.Sprintf("process-%s-step-%s", f.processID, stepID)
}

// ValidateStepConfiguration validates that a step configuration is valid
func (f *ProcessAgentFactory) ValidateStepConfiguration(
	step *EnhancedOrchestrationStep,
	agentDef types.AgentDefinition,
) error {
	// Validate required fields
	if step.ID == "" {
		return fmt.Errorf("step ID is required")
	}
	if step.AgentID == "" {
		return fmt.Errorf("agent ID is required for step %s", step.ID)
	}
	if agentDef.ID == "" {
		return fmt.Errorf("agent definition ID is required")
	}

	// Validate model availability
	model := step.GetModel(agentDef, f.defaultConfig.EditingModel)
	if model == "" {
		return fmt.Errorf("no model specified for step %s", step.ID)
	}

	// Validate tool requirements
	if step.AgentConfig != nil && len(step.AgentConfig.RequiredTools) > 0 {
		// TODO: Validate that required tools are available
		f.logger.LogProcessStep(fmt.Sprintf("Step %s requires tools: %v", step.ID, step.AgentConfig.RequiredTools))
	}

	return nil
}