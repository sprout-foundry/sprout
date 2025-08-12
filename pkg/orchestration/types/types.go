package types

import (
	"encoding/json"
	"strings"
)

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
