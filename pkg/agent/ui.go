package agent

import (
	"context"

)

// UI provides UI capabilities to the agent
type UI interface {
	// ShowDropdown displays a dropdown selection UI
	ShowDropdown(ctx context.Context, items interface{}, options DropdownOptions) (interface{}, error)

	// ShowQuickPrompt shows a small prompt with quick choices
	ShowQuickPrompt(ctx context.Context, prompt string, options []QuickOption, horizontal bool) (QuickOption, error)

	// IsInteractive returns true if UI is available
	IsInteractive() bool
}

// SetUI sets the UI provider for the agent
func (a *Agent) SetUI(ui UI) {
	a.ui = ui
}

// ShowDropdown shows a dropdown if UI is available
func (a *Agent) ShowDropdown(items interface{}, options DropdownOptions) (interface{}, error) {
	if a.ui == nil || !a.ui.IsInteractive() {
		return nil, ErrUINotAvailable
	}

	return a.ui.ShowDropdown(context.Background(), items, options)
}

// ShowQuickPrompt shows a quick prompt if UI is available
func (a *Agent) ShowQuickPrompt(prompt string, options []QuickOption, horizontal bool) (QuickOption, error) {
	if a.ui == nil || !a.ui.IsInteractive() {
		return QuickOption{}, ErrUINotAvailable
	}
	return a.ui.ShowQuickPrompt(context.Background(), prompt, options, horizontal)
}
