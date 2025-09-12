package orchestration

import (
	"fmt"
	"strings"
	
	"github.com/alantheprice/ledit/pkg/orchestration/types"
)

// AgentStepConfig defines agent-specific configuration for process steps
type AgentStepConfig struct {
	Model              string   `json:"model,omitempty"`              // Override agent model
	SystemPrompt       string   `json:"system_prompt,omitempty"`      // Step-specific system prompt
	MaxIterations      int      `json:"max_iterations,omitempty"`     // Limit agent iterations
	EnableWorkspace    bool     `json:"enable_workspace"`             // Use workspace intelligence
	EnableChangeTrack  bool     `json:"enable_change_track"`          // Track changes
	ContextScope       string   `json:"context_scope,omitempty"`      // "file", "directory", "project"
	RequiredTools      []string `json:"required_tools,omitempty"`     // Tools this step needs
	SafetyMode         string   `json:"safety_mode,omitempty"`        // "strict", "normal", "permissive"
}

// EnhancedOrchestrationStep extends the base OrchestrationStep with agent integration
type EnhancedOrchestrationStep struct {
	*types.OrchestrationStep // Embed the base step

	// NEW: Agent integration fields
	AgentConfig        *AgentStepConfig `json:"agent_config,omitempty"`
	WorkspaceScope     []string         `json:"workspace_scope,omitempty"`     // Files/dirs to focus on
	ToolRestrictions   []string         `json:"tool_restrictions,omitempty"`   // Allowed tools only
	SafetyMode         string           `json:"safety_mode,omitempty"`         // "strict", "normal", "permissive"
	EnableWorkspace    bool             `json:"enable_workspace"`              // Use workspace intelligence  
	EnableChangeTrack  bool             `json:"enable_change_track"`           // Enable change tracking via agent workflow
}

// ToEnhancedStep converts a base OrchestrationStep to an EnhancedOrchestrationStep
func ToEnhancedStep(baseStep *types.OrchestrationStep) *EnhancedOrchestrationStep {
	return &EnhancedOrchestrationStep{
		OrchestrationStep: baseStep,
		// Set reasonable defaults
		EnableWorkspace:   true, // Enable workspace intelligence by default
		EnableChangeTrack: true, // Enable change tracking by default
		SafetyMode:        "normal",
		AgentConfig: &AgentStepConfig{
			MaxIterations:     10,   // Reasonable default for process steps
			EnableWorkspace:   true, // Available via workspace integration
			EnableChangeTrack: true, // Available via agent workflow
			ContextScope:    "project", // Default to project-wide context
		},
	}
}

// GetModel returns the model to use for this step (step override > agent override > default)
func (s *EnhancedOrchestrationStep) GetModel(agentDef types.AgentDefinition, defaultModel string) string {
	// Priority: step config > agent config > default
	if s.AgentConfig != nil && s.AgentConfig.Model != "" {
		return s.AgentConfig.Model
	}
	if agentDef.Model != "" {
		return agentDef.Model
	}
	return defaultModel
}

// GetSystemPrompt returns the system prompt combining agent persona and step context
func (s *EnhancedOrchestrationStep) GetSystemPrompt(agentDef types.AgentDefinition) string {
	basePrompt := ""
	
	// Start with agent persona
	if agentDef.Persona != "" {
		basePrompt += fmt.Sprintf("You are a %s. ", agentDef.Persona)
	}
	
	if agentDef.Description != "" {
		basePrompt += fmt.Sprintf("%s ", agentDef.Description)
	}
	
	// Add skills if available
	if len(agentDef.Skills) > 0 {
		basePrompt += fmt.Sprintf("Your expertise includes: %s. ", strings.Join(agentDef.Skills, ", "))
	}
	
	// Add step-specific context
	if s.Description != "" {
		basePrompt += fmt.Sprintf("\nFor this step, you need to: %s", s.Description)
	}
	
	// Add step-specific system prompt if provided
	if s.AgentConfig != nil && s.AgentConfig.SystemPrompt != "" {
		basePrompt += fmt.Sprintf("\n\nAdditional instructions for this step: %s", s.AgentConfig.SystemPrompt)
	}
	
	return basePrompt
}

// ProcessContext provides context from the overall process to individual steps
type ProcessContext struct {
	ProcessID      string                 `json:"process_id"`
	Goal           string                 `json:"goal"`
	CurrentStep    int                    `json:"current_step"`
	TotalSteps     int                    `json:"total_steps"`
	PreviousOutput map[string]string      `json:"previous_output"`    // Output from previous steps
	ProcessState   map[string]interface{} `json:"process_state"`      // Shared process state
	WorkspaceInfo  interface{}            `json:"workspace_info"`     // Workspace context
}

// GetStepInput builds the complete input for a step including process context
func (s *EnhancedOrchestrationStep) GetStepInput(context ProcessContext) string {
	var inputParts []string
	
	// Add process context
	if context.Goal != "" {
		inputParts = append(inputParts, fmt.Sprintf("Overall Goal: %s", context.Goal))
	}
	
	if context.CurrentStep > 0 {
		inputParts = append(inputParts, fmt.Sprintf("Step %d of %d", context.CurrentStep, context.TotalSteps))
	}
	
	// Add step-specific input
	if len(s.Input) > 0 {
		inputParts = append(inputParts, "Step Input:")
		for key, value := range s.Input {
			inputParts = append(inputParts, fmt.Sprintf("- %s: %s", key, value))
		}
	}
	
	// Add previous output if available
	if len(context.PreviousOutput) > 0 {
		inputParts = append(inputParts, "Previous Step Output:")
		for key, value := range context.PreviousOutput {
			inputParts = append(inputParts, fmt.Sprintf("- %s: %s", key, value))
		}
	}
	
	// Add expected output
	if s.ExpectedOutput != "" {
		inputParts = append(inputParts, fmt.Sprintf("Expected Output: %s", s.ExpectedOutput))
	}
	
	return strings.Join(inputParts, "\n\n")
}