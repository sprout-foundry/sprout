//go:build !js

package webui

// terminal_create_options.go — the webui terminal-session option types
// (SessionOption, WithName, WithAutoClose) and the CreateHiddenSession entry
// point. Split out of terminal_create.go.

import (
	"fmt"
	"log/slog"
	"runtime"
	"strings"
)

// SessionOption is a functional option for configuring a terminal session.
type SessionOption func(*TerminalSession)

// WithName sets a human-readable name for the session.
func WithName(name string) SessionOption {
	return func(s *TerminalSession) {
		s.Name = strings.TrimSpace(name)
	}
}

// WithAutoClose sets whether the session should be auto-closed when inactive.
func WithAutoClose(autoClose bool) SessionOption {
	return func(s *TerminalSession) {
		s.AutoClose = autoClose
	}
}

// CreateHiddenSession creates a hidden PTY session for agent use.
// Hidden sessions are excluded from the default ListSessions() output
// but still participate in inactive-session cleanup.
//
// NOTE: Session creation runs while holding tm.mutex to prevent the PTY
// reader goroutine (launched by createUnixSession/createWindowsSession)
// from being visible to ListSessions() before the Hidden flag is set.
func (tm *TerminalManager) CreateHiddenSession(id, owner, chatID string, opts ...SessionOption) (session *TerminalSession, err error) {
	if err := validateSessionID(id); err != nil {
		return nil, err
	}
	owner = strings.TrimSpace(owner)
	chatID = strings.TrimSpace(chatID)
	if owner == "" {
		return nil, fmt.Errorf("hidden session owner is required")
	}
	if chatID == "" {
		return nil, fmt.Errorf("hidden session chatID is required")
	}

	tm.mutex.Lock()
	defer tm.mutex.Unlock()
	// NOTE: Session creation (including blocking PTY startup) happens while
	// holding tm.mutex. This serializes session creation but ensures the PTY
	// reader goroutine never sees a session with Hidden=false in the map.
	// For Phase A, this trade-off is acceptable. If creation latency becomes
	// a concern, consider two-phase creation with a "creating" sentinel state.

	// Check for duplicate session ID while holding the lock.
	if _, exists := tm.sessions[id]; exists {
		return nil, fmt.Errorf("%w: %s", ErrSessionExists, id)
	}
	if err := tm.checkTestSessionCap(); err != nil {
		return nil, err
	}

	// Panic recovery: if option application panics, clean up the PTY goroutine
	// to prevent a session leak.
	defer func() {
		if r := recover(); r != nil {
			webuiLogger.Error("hidden terminal session creation panicked", slog.String("session_id", id), slog.Any("panic", r))
			if session != nil {
				session.mutex.Lock()
				if session.Pty != nil {
					session.Pty.Close()
					session.Pty = nil
				}
				if session.Cancel != nil {
					session.Cancel()
					session.Cancel = nil
				}
				session.mutex.Unlock()
			}
			session = nil
			err = fmt.Errorf("CreateHiddenSession panic: %v", r)
		}
	}()

	// Create the underlying PTY session (without inserting into map).
	switch runtime.GOOS {
	case "windows":
		session, err = tm.createWindowsSession(id)
	default:
		session, err = tm.createUnixSession(id, "")
	}

	if err != nil {
		return nil, err
	}

	// Set hidden metadata before inserting into map.
	session.mutex.Lock()
	// Unlock in defer so the panic recover below can safely re-acquire the
	// lock to clean up Pty and Cancel fields.
	func() {
		defer session.mutex.Unlock()
		session.Hidden = true
		session.Owner = owner
		session.ChatID = chatID
		// AutoClose is reserved for SP-008 Phase B — not yet consumed by the
		// cleanup worker. When consumed, hidden sessions should auto-expire
		// after N minutes of inactivity.
		session.AutoClose = true // default for hidden sessions

		for _, opt := range opts {
			opt(session)
		}
	}()

	// Now insert into map with hidden flag already set.
	tm.sessions[id] = session

	return session, nil
}
