package types

import (
	"encoding/json"
	"strings"
)

// CodeReviewResult represents the result of an automated code review.
type CodeReviewResult struct {
	Status       string `json:"status"`               // "approved", "needs_revision", "rejected"
	Feedback     string `json:"feedback"`             // Explanation for the status
	Instructions string `json:"-"`                    // Instructions for `ledit` if status is "needs_revision"
	NewPrompt    string `json:"new_prompt,omitempty"` // A new, more detailed prompt if status is "rejected"
}

// UnmarshalJSON implements custom JSON unmarshaling to handle instructions field
// that can be either a string or an array of strings
func (c *CodeReviewResult) UnmarshalJSON(data []byte) error {
	// First unmarshal into a temporary struct with raw JSON for instructions
	type Alias CodeReviewResult
	aux := &struct {
		Instructions json.RawMessage `json:"instructions,omitempty"`
		*Alias
	}{
		Alias: (*Alias)(c),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	// Handle the instructions field - it could be a string or array of strings
	if len(aux.Instructions) > 0 {
		// Try to unmarshal as string first
		var instructionsStr string
		if err := json.Unmarshal(aux.Instructions, &instructionsStr); err == nil {
			c.Instructions = instructionsStr
		} else {
			// Try to unmarshal as array of strings
			var instructionsArray []string
			if err := json.Unmarshal(aux.Instructions, &instructionsArray); err == nil {
				// Join array elements with newlines
				c.Instructions = strings.Join(instructionsArray, "\n")
			} else {
				// If both fail, convert the raw JSON to string
				c.Instructions = string(aux.Instructions)
			}
		}
	}

	return nil
}

// MultiAgentOrchestrationPlan represents a plan that involves multiple agents with different personas
type MultiAgentOrchestrationPlan struct {
	Goal          string                 `json:"goal"`           // Overall goal description
	BaseModel     string                 `json:"base_model"`     // Default model for agents (can be overridden per agent)
	Agents        []AgentDefinition      `json:"agents"`         // List of agents to run
	Steps         []OrchestrationStep    `json:"steps"`          // Sequential steps for the agents
	CurrentStep   int                    `json:"current_step"`   // Current step being executed
	Status        string                 `json:"status"`         // "pending", "in_progress", "completed", "failed"
	AgentStatuses map[string]AgentStatus `json:"agent_statuses"` // Status of each agent
	Attempts      int                    `json:"attempts"`       // Number of attempts made
	LastError     string                 `json:"last_error"`     // Last error encountered
	CreatedAt     string                 `json:"created_at"`     // When the plan was created
	CompletedAt   string                 `json:"completed_at"`   // When the plan was completed
}

// AgentDefinition defines an agent with a specific persona and responsibilities
type AgentDefinition struct {
	ID          string            `json:"id"`          // Unique identifier for the agent
	Name        string            `json:"name"`        // Human-readable name
	Persona     string            `json:"persona"`     // Role/persona (e.g., "frontend_developer", "backend_architect", "qa_engineer")
	Description string            `json:"description"` // What this agent is responsible for
	Skills      []string          `json:"skills"`      // List of skills/expertise areas
	Model       string            `json:"model"`       // Which LLM model to use for this agent
	Priority    int               `json:"priority"`    // Execution priority (lower = higher priority)
	DependsOn   []string          `json:"depends_on"`  // Agent IDs this agent depends on
	Config      map[string]string `json:"config"`      // Agent-specific configuration
	Budget      *AgentBudget      `json:"budget"`      // Budget constraints for this agent
}

// OrchestrationStep represents a step in the multi-agent orchestration
type OrchestrationStep struct {
	ID             string            `json:"id"`              // Unique identifier for the step
	Name           string            `json:"name"`            // Human-readable name
	Description    string            `json:"description"`     // What this step accomplishes
	AgentID        string            `json:"agent_id"`        // Which agent should execute this step
	Input          map[string]string `json:"input"`           // Input data for the agent
	ExpectedOutput string            `json:"expected_output"` // What output is expected
	Status         string            `json:"status"`          // "pending", "in_progress", "completed", "failed"
	Result         *StepResult       `json:"result"`          // Result of the step execution
	DependsOn      []string          `json:"depends_on"`      // Step IDs this step depends on
	Timeout        int               `json:"timeout"`         // Timeout in seconds
	Retries        int               `json:"retries"`         // Number of retries allowed
}

// StepResult represents the result of executing a step
type StepResult struct {
	Status     string            `json:"status"`      // "success", "failure", "partial_success"
	Output     map[string]string `json:"output"`      // Output data from the agent
	Files      []string          `json:"files"`       // Files created/modified
	Errors     []string          `json:"errors"`      // Any errors encountered
	Warnings   []string          `json:"warnings"`    // Any warnings
	Duration   float64           `json:"duration"`    // Time taken in seconds
	TokenUsage *AgentTokenUsage  `json:"token_usage"` // Token usage for this step
	Logs       []string          `json:"logs"`        // Execution logs
}

// AgentStatus tracks the current status of an agent
type AgentStatus struct {
	Status      string            `json:"status"`       // "idle", "working", "completed", "failed", "waiting"
	CurrentStep string            `json:"current_step"` // ID of the step currently being worked on
	Progress    int               `json:"progress"`     // Progress percentage (0-100)
	LastUpdate  string            `json:"last_update"`  // When status was last updated
	Errors      []string          `json:"errors"`       // Any errors encountered
	Output      map[string]string `json:"output"`       // Latest output from the agent
	TokenUsage  int               `json:"token_usage"`  // Total tokens used by this agent
	Cost        float64           `json:"cost"`         // Total cost incurred by this agent
}

// AgentTokenUsage tracks token usage for a specific agent
type AgentTokenUsage struct {
	AgentID    string `json:"agent_id"`
	Prompt     int    `json:"prompt"`
	Completion int    `json:"completion"`
	Total      int    `json:"total"`
	Model      string `json:"model"`
}

// AgentBudget defines budget constraints for an agent
type AgentBudget struct {
	MaxTokens    int     `json:"max_tokens"`     // Maximum tokens this agent can use
	MaxCost      float64 `json:"max_cost"`       // Maximum cost in USD this agent can incur
	TokenWarning int     `json:"token_warning"`  // Token threshold for warnings
	CostWarning  float64 `json:"cost_warning"`   // Cost threshold for warnings
	AlertOnLimit bool    `json:"alert_on_limit"` // Whether to alert when approaching limits
	StopOnLimit  bool    `json:"stop_on_limit"`  // Whether to stop execution when limit reached
}

// ProcessFile represents the structure of a process file that defines multi-agent orchestration
type ProcessFile struct {
	Version     string              `json:"version"`     // Version of the process file format
	Goal        string              `json:"goal"`        // Overall goal to achieve
	Description string              `json:"description"` // Detailed description of the goal
	BaseModel   string              `json:"base_model"`  // Default model for agents (can be overridden per agent)
	Agents      []AgentDefinition   `json:"agents"`      // Agents involved in the process
	Steps       []OrchestrationStep `json:"steps"`       // Steps to execute
	Validation  *ValidationConfig   `json:"validation"`  // Validation configuration
	Settings    *ProcessSettings    `json:"settings"`    // Process-wide settings
}

// ValidationConfig defines how to validate the process results
type ValidationConfig struct {
	BuildCommand string   `json:"build_command"` // Command to build the project
	TestCommand  string   `json:"test_command"`  // Command to run tests
	LintCommand  string   `json:"lint_command"`  // Command to run linting
	CustomChecks []string `json:"custom_checks"` // Custom validation commands
	Required     bool     `json:"required"`      // Whether validation is required
}

// ProcessSettings contains process-wide configuration
type ProcessSettings struct {
	MaxRetries        int    `json:"max_retries"`        // Maximum retries for failed steps
	StepTimeout       int    `json:"step_timeout"`       // Default timeout for steps in seconds
	ParallelExecution bool   `json:"parallel_execution"` // Whether steps can run in parallel
	StopOnFailure     bool   `json:"stop_on_failure"`    // Whether to stop on first failure
	LogLevel          string `json:"log_level"`          // Logging level
}
