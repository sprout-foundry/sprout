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

// ErrNoInteractiveSurface is returned when the password prompter cannot
// present a prompt to the user (e.g., stdin is not a TTY and no WebUI is
// connected).
var ErrNoInteractiveSurface = errors.New("no interactive surface available for password prompt")

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
