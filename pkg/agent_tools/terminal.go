package tools

import "context"

// TerminalAccess abstracts the operations that shell command execution needs
// from a terminal manager. This interface is satisfied by the webui's
// TerminalManager struct (pkg/webui/terminal_types.go) — no explicit import
// is needed; Go satisfies interfaces structurally.
//
// When a TerminalAccess is available in the context (WebUI mode), shell
// commands can route through hidden PTY sessions. When absent (CLI mode),
// commands use the existing os/exec path unchanged.
type TerminalAccess interface {
	// ExecuteCommandInHidden runs a command synchronously on a hidden PTY session
	// and returns the output and exit code.
	ExecuteCommandInHidden(ctx context.Context, sessionID string, command string) (output string, exitCode int, err error)

	// GetOrCreateHiddenSessionForChat returns the session ID of an existing hidden session
	// for the given chat, or creates a new one. Returns the session ID.
	GetOrCreateHiddenSessionForChat(ctx context.Context, chatID string) (sessionID string, err error)
}

// contextKey is an unexported type for context keys defined in this package.
type contextKey string

const terminalManagerKey contextKey = "terminalManager"

// WithTerminalManager returns a new context that carries the TerminalAccess.
// Use TerminalManagerFromContext to retrieve it.
func WithTerminalManager(ctx context.Context, tm TerminalAccess) context.Context {
	return context.WithValue(ctx, terminalManagerKey, tm)
}

// TerminalManagerFromContext extracts the TerminalAccess from the context.
// Returns nil if no terminal manager is available (CLI mode).
func TerminalManagerFromContext(ctx context.Context) TerminalAccess {
	if tm, ok := ctx.Value(terminalManagerKey).(TerminalAccess); ok {
		return tm
	}
	return nil
}
