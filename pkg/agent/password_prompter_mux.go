package agent

import (
	"context"
	"errors"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// cascadingPasswordPrompter tries prompters in order and returns the first
// successful password. Used to chain the WebUI prompter (browser dialog)
// with the CLI prompter (terminal ReadPassword) so the user gets prompted
// on whichever surface they're watching.
//
// Why both: an LLM-driven shell command that triggers sudo has no way to
// read a TTY password itself — it needs the user. If the WebUI is open,
// route there (better UX, no terminal interaction required). If the WebUI
// is closed or returns ErrNoInteractiveSurface, fall back to the terminal
// prompt (which works for `sprout` invoked from a real shell).
type cascadingPasswordPrompter struct {
	prompters []tools.PasswordPrompter
}

// Compile-time check that the cascading prompter satisfies the interface.
// Catches signature drift if the PasswordPrompter interface changes.
var _ tools.PasswordPrompter = (*cascadingPasswordPrompter)(nil)

// NewCascadingPasswordPrompter returns a prompter that tries each
// candidate in order, stopping on the first non-ErrNoInteractiveSurface
// result. Pass at least one prompter; the result is undefined for none.
//
// Exported because cmd/agent_modes.go composes the mux after
// agent_creation.go has already registered the CLI prompter — both
// packages need to reference this constructor.
func NewCascadingPasswordPrompter(prompters ...tools.PasswordPrompter) *cascadingPasswordPrompter {
	return &cascadingPasswordPrompter{prompters: prompters}
}

// Prompt asks each prompter in turn. Returns the first password and nil,
// or the last error if every prompter failed. Context cancellation is
// honored before the first call so a pre-cancelled context never blocks.
func (c *cascadingPasswordPrompter) Prompt(ctx context.Context, reason string) (string, error) {
	if len(c.prompters) == 0 {
		return "", tools.ErrNoInteractiveSurface
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	var lastErr error
	for _, p := range c.prompters {
		password, err := p.Prompt(ctx, reason)
		if err == nil {
			return password, nil
		}
		// Skip prompters that have no interactive surface — that's the
		// expected signal that the next candidate should be tried. Any
		// other error (timeout, ctx cancellation, channel closed) is
		// fatal: surface it instead of silently trying the next surface,
		// which would let a stale request block on the wrong UI.
		if errors.Is(err, tools.ErrNoInteractiveSurface) {
			lastErr = err
			continue
		}
		return "", err
	}
	return "", lastErr
}