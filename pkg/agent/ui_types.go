package agent

import (
	"fmt"
	"time"
)

// DropdownItem represents an item in a dropdown selection
type DropdownItem struct {
	Label string
	Value string
}

// DropdownOptions provides options for dropdown display
type DropdownOptions struct {
	Prompt       string
	SearchPrompt string
	ShowCounts   bool
}

// QuickOption represents a quick choice option
type QuickOption struct {
	Label string
	Value string
}

// UI errors
var (
	ErrUINotAvailable = fmt.Errorf("UI not available")
	ErrCancelled      = fmt.Errorf("user cancelled")
)

// SessionItem represents a session in dropdown selections
type SessionItem struct {
	Label       string
	Value       string
	SessionID   string
	Model       string
	LastUpdated time.Time
	Name        string // Human-readable session name
}

// ModelItem represents a model in dropdown selections
type ModelItem struct {
	Label         string
	Value         string
	Provider      string
	Model         string
	InputCost     float64
	OutputCost    float64
	LegacyCost    float64
	ContextLength int
	Tags          []string
}

// PublishModel publishes a model selection (placeholder implementation)
func PublishModel(model string) {
	// This would publish to some UI component, but for now it's a placeholder
	// Could be expanded to integrate with console or web UI
	fmt.Printf("Publishing model: %s\n", model)
}
