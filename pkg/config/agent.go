package config

// AgentConfig contains all agent behavior and orchestration related configuration
type AgentConfig struct {
	// Orchestration Settings
	OrchestrationMaxAttempts int    `json:"orchestration_max_attempts"` // Maximum attempts for orchestration
	PolicyVariant            string `json:"policy_variant"`             // Policy variant to use
	AutoGenerateTests        bool   `json:"auto_generate_tests"`        // Auto-generate tests after changes

	// Agent Behavior
	DryRun           bool `json:"dry_run"`            // Run agents in dry-run mode
	FromAgent        bool `json:"-"`                  // Internal flag: true when invoked from agent mode
	CodeToolsEnabled bool `json:"code_tools_enabled"` // Enable/disable code tools enrichment

	// Code Style
	CodeStyle CodeStylePreferences `json:"code_style"` // Code style preferences
}

// CodeStylePreferences defines the preferred code style guidelines for the project
type CodeStylePreferences struct {
	FunctionSize      string `json:"function_size"`      // Preferred function size
	FileSize          string `json:"file_size"`          // Preferred file size
	NamingConventions string `json:"naming_conventions"` // Naming convention preferences
	ErrorHandling     string `json:"error_handling"`     // Error handling style
	TestingApproach   string `json:"testing_approach"`   // Testing approach
	Modularity        string `json:"modularity"`         // Modularity preferences
	Readability       string `json:"readability"`        // Readability preferences
}

// DefaultAgentConfig returns sensible defaults for agent configuration
func DefaultAgentConfig() *AgentConfig {
	return &AgentConfig{
		OrchestrationMaxAttempts: 3,
		PolicyVariant:            "balanced",
		AutoGenerateTests:        false,
		DryRun:                   false,
		FromAgent:                false,
		CodeToolsEnabled:         true, // Enable code tools by default
		CodeStyle: CodeStylePreferences{
			FunctionSize:      "medium",
			FileSize:          "large",
			NamingConventions: "camelCase",
			ErrorHandling:     "comprehensive",
			TestingApproach:   "unit_tests",
			Modularity:        "high",
			Readability:       "high",
		},
	}
}

// GetMaxRetries returns the maximum number of retries for agent operations
func (c *AgentConfig) GetMaxRetries() int {
	if c.OrchestrationMaxAttempts < 1 {
		return 3 // default
	}
	return c.OrchestrationMaxAttempts
}

// IsProductionReady returns true if the agent is configured for production use
func (c *AgentConfig) IsProductionReady() bool {
	return !c.DryRun && c.PolicyVariant != ""
}

// ShouldGenerateTests returns true if tests should be auto-generated
func (c *AgentConfig) ShouldGenerateTests() bool {
	return c.AutoGenerateTests && !c.DryRun
}
