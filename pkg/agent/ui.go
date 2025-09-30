package agent

import (
	"context"

	"github.com/alantheprice/ledit/pkg/ui"
)

// UI provides UI capabilities to the agent
type UI interface {
	// ShowDropdown displays a dropdown selection UI
	ShowDropdown(ctx context.Context, items []ui.DropdownItem, options ui.DropdownOptions) (ui.DropdownItem, error)

	// ShowQuickPrompt shows a small prompt with quick choices
	ShowQuickPrompt(ctx context.Context, prompt string, options []ui.QuickOption, horizontal bool) (ui.QuickOption, error)

	// IsInteractive returns true if UI is available
	IsInteractive() bool
}

// SetUI sets the UI provider for the agent
func (a *Agent) SetUI(ui UI) {
	a.ui = ui
}

// ShowDropdown shows a dropdown if UI is available
func (a *Agent) ShowDropdown(items []ui.DropdownItem, options ui.DropdownOptions) (ui.DropdownItem, error) {
	if a.ui == nil || !a.ui.IsInteractive() {
		return nil, ui.ErrUINotAvailable
	}

	return a.ui.ShowDropdown(context.Background(), items, options)
}

// ShowQuickPrompt shows a quick prompt if UI is available
func (a *Agent) ShowQuickPrompt(prompt string, options []ui.QuickOption, horizontal bool) (ui.QuickOption, error) {
	if a.ui == nil || !a.ui.IsInteractive() {
		return ui.QuickOption{}, ui.ErrUINotAvailable
	}
	return a.ui.ShowQuickPrompt(context.Background(), prompt, options, horizontal)
}
