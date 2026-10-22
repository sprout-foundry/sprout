package tools

import (
	"context"
	"errors"
)

// PasswordPrompter handles interactive password prompts during shell command
// execution. The interface is defined in pkg/agent_tools (the consumer
// package) so that both pkg/agent_tools (shell tool) and pkg/agent (broker +
// CLI impl) can reference it without import cycles. Implementors in other
// packages satisfy it structurally — no explicit import is needed.
type PasswordPrompter interface {
	// Prompt asks the user to type a password and returns it without a
	// trailing newline. The reason is a human-readable description shown to
	// the user (e.g., "sudo apt update needs your password").
	//
	// Returns ErrNoInteractiveSurface when there is no way to prompt the
	// user (non-TTY stdin, no WebUI client, etc.).
	Prompt(ctx context.Context, reason string) (string, error)
}

// InteractivePrompter is implemented by prompters that can also collect a
// non-secret answer — a confirmation ("Overwrite? [y/N]") or a value ("Enter a
// name:"). The shell scanner calls PromptInput when it detects such a prompt
// and falls back to Prompt when the prompter does not implement this.
//
// secret tells the surface whether to mask the input and redact the answer.
type InteractivePrompter interface {
	PromptInput(ctx context.Context, reason string, secret bool) (string, error)
}

// ErrNoInteractiveSurface is returned when the password prompter cannot
// present a prompt to the user (e.g., stdin is not a TTY and no WebUI is
// connected).
var ErrNoInteractiveSurface = errors.New("no interactive surface available for password prompt")

// ErrPromptCancelled is returned when the user dismissed an interactive
// prompt. The shell scanner closes the child's stdin so the command stops,
// instead of writing a blank line that a prompt would accept as its default.
var ErrPromptCancelled = errors.New("interactive prompt cancelled by user")

// passwordPrompterKey is an unexported context key type.
type passwordPrompterKey struct{}

// WithPasswordPrompter returns a new context that carries the PasswordPrompter.
// Use PasswordPrompterFromContext to retrieve it.
func WithPasswordPrompter(ctx context.Context, pp PasswordPrompter) context.Context {
	return context.WithValue(ctx, passwordPrompterKey{}, pp)
}

// PasswordPrompterFromContext extracts the PasswordPrompter from the context.
// Returns nil if no prompter is available.
func PasswordPrompterFromContext(ctx context.Context) PasswordPrompter {
	if pp, ok := ctx.Value(passwordPrompterKey{}).(PasswordPrompter); ok {
		return pp
	}
	return nil
}

// PromptForAnswer asks pp for an answer to a shell prompt. When the prompter
// supports interactive input it is told whether the answer is secret;
// otherwise Prompt is used, which surfaces a password-shaped request.
func PromptForAnswer(ctx context.Context, pp PasswordPrompter, reason string, secret bool) (string, error) {
	if ip, ok := pp.(InteractivePrompter); ok {
		return ip.PromptInput(ctx, reason, secret)
	}
	return pp.Prompt(ctx, reason)
}
