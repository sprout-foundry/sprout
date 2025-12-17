package agent

import (
	"context"
)

// SimpleUI provides a minimal fallback UI implementation
type SimpleUI struct{}

// NewSimpleUI creates a new simple UI instance
func NewSimpleUI() *SimpleUI {
	return &SimpleUI{}
}

// IsInteractive returns false for simple UI (non-interactive)
func (s *SimpleUI) IsInteractive() bool {
	return false
}

// ShowDropdown returns an error since simple UI doesn't support dropdowns
func (s *SimpleUI) ShowDropdown(ctx context.Context, items interface{}, options DropdownOptions) (interface{}, error) {
	return nil, ErrUINotAvailable
}

// ShowQuickPrompt returns an error since simple UI doesn't support prompts
func (s *SimpleUI) ShowQuickPrompt(ctx context.Context, prompt string, options []QuickOption, horizontal bool) (QuickOption, error) {
	return QuickOption{}, ErrUINotAvailable
}