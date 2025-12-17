package agent

import (
	"context"
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
		return "", ErrUINotAvailable
	}

	// Prefer quick prompt when available
	qopts := make([]QuickOption, 0, len(choices))
	for _, c := range choices {
		qopts = append(qopts, QuickOption{Label: c.Label, Value: c.Value})
	}
	if qp, err := a.ui.ShowQuickPrompt(context.Background(), prompt, qopts, true); err == nil {
		return qp.Value, nil
	}

	// Fallback to dropdown
	items := make([]DropdownItem, 0, len(choices))
	for _, c := range choices {
		items = append(items, DropdownItem{Label: c.Label, Value: c.Value})
	}
	selected, err := a.ui.ShowDropdown(context.Background(), items, DropdownOptions{Prompt: prompt})
	if err != nil {
		return "", err
	}
	dropdownItem := selected.(DropdownItem)
	return dropdownItem.Value, nil
}
