package types

// OrchestrationPlan represents the overall plan for a feature, broken down into requirements.
type OrchestrationPlan struct {
	Prompt       string        `json:"prompt"`
	Requirements []Requirement `json:"requirements"`
	CurrentStep  int           `json:"current_step"`
	Status       string        `json:"status"` // e.g., "pending", "in_progress", "completed", "failed"
	Attempts     int           `json:"attempts"`
	LastError    string        `json:"last_error"`
}

// Requirement represents a single step or task in the orchestration plan.
type Requirement struct {
	ID               string                `json:"id"`
	Instruction      string                `json:"instruction"` // Renamed from Description
	Status           string                `json:"status"`      // e.g., "pending", "in_progress", "completed", "failed"
	Changes          []OrchestrationChange `json:"changes"`
	Attempts         int                   `json:"attempts"`
	LastError        string                `json:"last_error"`
	ValidationScript string                `json:"validation_script,omitempty"` // Optional script to run after changes
}

// OrchestrationChange represents a specific file modification or creation suggested by the LLM for a requirement.
type OrchestrationChange struct {
	Filepath                 string `json:"filepath"`                             // Renamed from Filename
	Instruction              string `json:"instruction"`                          // Renamed from Instructions
	Status                   string `json:"status"`                               // e.g., "pending", "applied", "skipped", "failed"
	Diff                     string `json:"diff,omitempty"`                       // Optional: store the diff for review
	Error                    string `json:"error,omitempty"`                      // Optional: store error if applying failed
	ValidationFailureContext string `json:"validation_failure_context,omitempty"` // Context from validation script failure
	LastLLMResponse          string `json:"last_llm_response,omitempty"`          // Last LLM response for this change
}

// OrchestrationChangesList is a helper struct for JSON unmarshalling of a list of changes.
type OrchestrationChangesList struct {
	Changes []OrchestrationChange `json:"changes"`
}
