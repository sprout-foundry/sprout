package editor

// WorkspaceFile represents the structure of the workspace.json file.
type WorkspaceFile struct {
	Files map[string]WorkspaceFileInfo `json:"files"`
}

// WorkspaceFileInfo stores information about a single file in the workspace.
type WorkspaceFileInfo struct {
	Hash       string `json:"hash"`
	Summary    string `json:"summary"`
	Exports    string `json:"exports"`
	References string `json:"references"`
	TokenCount int    `json:"token_count"`
}

// OrchestrationRequirement defines a single step in the orchestration plan.
type OrchestrationRequirement struct {
	Filepath                 string `json:"filepath"`
	Instruction              string `json:"instruction"`
	Status                   string `json:"status"` // "pending", "completed", "failed"
	ValidationFailureContext string `json:"validation_failure_context,omitempty"`
	LastLLMResponse          string `json:"last_llm_response,omitempty"`
}

// OrchestrationPlan holds the entire list of requirements for a feature.
type OrchestrationPlan struct {
	Requirements []OrchestrationRequirement `json:"requirements"`
}
