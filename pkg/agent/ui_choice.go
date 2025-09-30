package agent

import (
	"context"
	"github.com/alantheprice/ledit/pkg/ui"
)

// ChoiceOption represents a simple label/value option for UI prompts
type ChoiceOption struct {
	Label string
	Value string
}

// choiceDropdownItem adapts ChoiceOption to ui.DropdownItem
type choiceDropdownItem struct{ opt ChoiceOption }

func (i choiceDropdownItem) Display() string    { return i.opt.Label }
func (i choiceDropdownItem) SearchText() string { return i.opt.Label }
func (i choiceDropdownItem) Value() interface{} { return i.opt.Value }

// PromptChoice shows a dropdown selection of simple choices and returns the selected value
func (a *Agent) PromptChoice(prompt string, choices []ChoiceOption) (string, error) {
	if a.ui == nil || !a.ui.IsInteractive() {
		return "", ui.ErrUINotAvailable
	}

	// Prefer quick prompt when available
	qopts := make([]ui.QuickOption, 0, len(choices))
	for _, c := range choices {
		qopts = append(qopts, ui.QuickOption{Label: c.Label, Value: c.Value})
	}
	if qp, err := a.ui.ShowQuickPrompt(context.Background(), prompt, qopts, true); err == nil {
		return qp.Value, nil
	}

	// Fallback to dropdown
	items := make([]ui.DropdownItem, 0, len(choices))
	for _, c := range choices {
		items = append(items, choiceDropdownItem{opt: c})
	}
	selected, err := a.ui.ShowDropdown(context.Background(), items, ui.DropdownOptions{Prompt: prompt})
	if err != nil {
		return "", err
	}
	if v, ok := selected.Value().(string); ok {
		return v, nil
	}
	return "", ui.ErrCancelled
}
