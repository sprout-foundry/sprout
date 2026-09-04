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

	// ExecuteCommandInBackground writes a command to a new hidden PTY session
	// and returns immediately with the session ID. Does NOT wait for completion.
	// Background sessions get a descriptive name and longer cleanup timeout.
	// The command is wrapped with a completion sentinel so its exit can be
	// observed via BackgroundDoneChan even though the shell outlives it.
	ExecuteCommandInBackground(ctx context.Context, chatID, command string) (sessionID string, err error)

	// GetBackgroundOutput returns accumulated output for a background session.
	GetBackgroundOutput(sessionID string) (output string, err error)

	// StopBackgroundSession terminates a background session by session ID.
	// Sends Ctrl+C to the PTY and closes the session. Returns an error if the
	// session is not found or is not a background session.
	StopBackgroundSession(sessionID string) error

	// IsSessionActive checks whether a session (by ID) is still active.
	// Returns false if the session doesn't exist or has terminated.
	IsSessionActive(sessionID string) bool

	// BackgroundDoneChan returns a channel that closes exactly once, when the
	// background COMMAND in sessionID completes — not when the session dies.
	// Background commands run inside a long-lived shell, so session liveness
	// is NOT a completion signal; this channel is. The second return is false
	// when the session is unknown (never created, or already reaped).
	//
	// The channel also closes when the session ends before the command
	// produces its sentinel; BackgroundExitCode reports that case.
	BackgroundDoneChan(sessionID string) (<-chan struct{}, bool)

	// BackgroundExitCode returns the command's exit code. Valid only after
	// the channel from BackgroundDoneChan closes. >= 0 is a real exit code;
	// BgExitNone means the session ended without a sentinel (reaped, killed);
	// BgExitStopped means the command was stopped via StopBackgroundSession.
	BackgroundExitCode(sessionID string) int
}

// Background exit code sentinels for TerminalAccess implementations that
// cannot observe a real exit code (PTY sessions whose shell outlives the
// command). Values are deliberately below the valid 0–255 exit-code range.
const (
	// BgExitNone: the session ended before the command's completion sentinel
	// was observed (reaped by cleanup, killed, or the PTY died).
	BgExitNone = -1
	// BgExitStopped: the command was deliberately stopped via
	// StopBackgroundSession rather than running to completion.
	BgExitStopped = -2
)

// contextKey is an unexported type for context keys defined in this package.
type contextKey string

const terminalManagerKey contextKey = "terminalManager"

// shellChatIDKey carries the conversation-scoped identifier used by hidden
// and background shell sessions. Set by the tool layer from ToolEnv.ChatID
// (which WebUI agents populate from their event metadata) so sessions are
// scoped per chat instead of all sharing the "default" bucket.
const shellChatIDKey contextKey = "shellChatID"

// WithShellChatID returns a context whose shell sessions are scoped to chatID.
func WithShellChatID(ctx context.Context, chatID string) context.Context {
	return context.WithValue(ctx, shellChatIDKey, chatID)
}

// ShellChatIDFromContext extracts the conversation-scoped shell chat ID.
// Empty when unset (CLI mode, tests).
func ShellChatIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(shellChatIDKey).(string); ok {
		return id
	}
	return ""
}

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
