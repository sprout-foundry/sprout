package agent

import (
	"context"
	"errors"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
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

// PromptChoice asks the user to pick one of choices and returns its value.
//
// With no interactive a.ui — the CLI and the WebUI never install one — the
// question goes through the ask_user surface, which reaches a connected
// browser tab or the terminal picker (with the steer, spinner and
// one-question guards). Returning ErrUINotAvailable there instead, as this
// did before, meant every caller's "no UI" branch ran in an interactive
// session: commit and create_pr proceeded without asking, and secret
// detection redacted without asking. ErrUINotAvailable now means what it
// says — nobody can be asked (an automate/workflow run, no terminal and no
// browser).
func (a *Agent) PromptChoice(prompt string, choices []ChoiceOption) (string, error) {
	if a.ui == nil || !a.ui.IsInteractive() {
		return a.promptChoiceViaAsk(prompt, choices)
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
		return "", agenterrors.NewTransientError("failed to show dropdown", err)
	}
	dropdownItem := selected.(DropdownItem)
	return dropdownItem.Value, nil
}

// promptChoiceAsk is the ask_user call PromptChoice falls back to,
// indirected so tests can answer it without a terminal.
var promptChoiceAsk = func(a *Agent, req tools.AskUserRequest) (string, error) {
	return newAgentAskUserService(a).Ask(a.InterruptCtx(), req)
}

func (a *Agent) promptChoiceViaAsk(prompt string, choices []ChoiceOption) (string, error) {
	if a.isNonInteractive() && !a.HasActiveWebUIClients() {
		return "", ErrUINotAvailable
	}
	opts := make([]tools.AskUserOption, 0, len(choices))
	for _, c := range choices {
		opts = append(opts, tools.AskUserOption{Label: c.Label, Value: c.Value})
	}
	answer, err := promptChoiceAsk(a, tools.AskUserRequest{Header: "Approval", Question: prompt, Options: opts})
	if err != nil {
		if errors.Is(err, tools.ErrAskUserNoChannel) {
			return "", ErrUINotAvailable
		}
		if errors.Is(err, tools.ErrAskUserDismissed) {
			return "", nil // dismissed: no choice, i.e. not approved
		}
		return "", err
	}
	// The terminal picker also accepts a typed answer; only a listed value
	// is a choice. Anything else is no choice, which every caller treats as
	// "not approved".
	for _, c := range choices {
		if answer == c.Value {
			return answer, nil
		}
	}
	return "", nil
}
