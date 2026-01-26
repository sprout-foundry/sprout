package spec

import (
	"time"
)

// Message represents a conversation message
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// CanonicalSpec represents the extracted specification from conversation
type CanonicalSpec struct {
	ID           string    `json:"id"`           // Unique identifier
	CreatedAt    time.Time `json:"created_at"`   // When spec was extracted
	UserPrompt   string    `json:"user_prompt"`  // Original user request
	Objective    string    `json:"objective"`    // Clear objective statement
	InScope      []string  `json:"in_scope"`     // What's included
	OutOfScope   []string  `json:"out_of_scope"` // What's explicitly excluded
	Acceptance   []string  `json:"acceptance"`   // Acceptance criteria
	Context      string    `json:"context"`      // Additional context from conversation
	Conversation []Message `json:"conversation"` // Full conversation for reference
}

// SpecExtractionResult represents result of spec extraction
type SpecExtractionResult struct {
	Spec       *CanonicalSpec `json:"spec"`
	Confidence float64        `json:"confidence"` // 0-1 score
	Reasoning  string         `json:"reasoning"`  // How spec was derived
}

// ScopeViolation represents a scope violation found during review
type ScopeViolation struct {
	File        string `json:"file"`        // File where violation found
	Line        int    `json:"line"`        // Line number
	Type        string `json:"type"`        // "addition", "modification", "removal"
	Severity    string `json:"severity"`    // "critical", "high", "medium", "low"
	Description string `json:"description"` // What was added/changed
	Why         string `json:"why"`         // Why it's out of scope
}

// ScopeReviewResult represents result of scope validation
type ScopeReviewResult struct {
	InScope     bool             `json:"in_scope"`     // Overall pass/fail
	Violations  []ScopeViolation `json:"violations"`   // Specific violations
	Summary     string           `json:"summary"`      // Human-readable summary
	Suggestions []string         `json:"suggestions"`  // How to fix violations
}
