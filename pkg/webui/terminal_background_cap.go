//go:build !js

package webui

import (
	"fmt"
)

// maxBackgroundSessionsPerChat caps the number of concurrent background
// PTY sessions a single chat may own. Each background session consumes a
// PTY (file descriptors, kernel struct), a goroutine watching its output,
// and an in-memory scrollback buffer for up to its 2-hour cleanup window.
// Without a cap, a misbehaving agent that promotes (or explicitly starts)
// many slow commands can pile sessions up indefinitely; at this cap the
// agent is forced to either wait/poll the existing ones or call
// stop_background before starting another.
//
// The cap is intentionally generous for normal agent work (typically
// 1–2 long-running commands at once) but firm enough to prevent runaway
// resource exhaustion.
const maxBackgroundSessionsPerChat = 5

// countBackgroundSessionsForChat returns the number of currently-active
// background PTY sessions owned by the given chat ID.
func (tm *TerminalManager) countBackgroundSessionsForChat(chatID string) int {
	if chatID == "" {
		return 0
	}
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()
	count := 0
	for _, session := range tm.sessions {
		session.mutex.RLock()
		if session.IsBackground && session.Active && session.ChatID == chatID {
			count++
		}
		session.mutex.RUnlock()
	}
	return count
}

// listBackgroundSessionIDsForChat returns the IDs of active background
// sessions for the given chat — useful for surfacing in cap-rejection
// errors so the agent can pick one to stop_background.
func (tm *TerminalManager) listBackgroundSessionIDsForChat(chatID string) []string {
	if chatID == "" {
		return nil
	}
	tm.mutex.RLock()
	defer tm.mutex.RUnlock()
	var ids []string
	for _, session := range tm.sessions {
		session.mutex.RLock()
		if session.IsBackground && session.Active && session.ChatID == chatID {
			ids = append(ids, session.ID)
		}
		session.mutex.RUnlock()
	}
	return ids
}

// errBackgroundCapReached formats a clear, actionable error for the agent
// when it tries to create a new background session while at the per-chat
// cap. The agent sees the existing session IDs so it can decide which
// to stop_background before retrying.
func (tm *TerminalManager) errBackgroundCapReached(chatID string) error {
	existing := tm.listBackgroundSessionIDsForChat(chatID)
	return fmt.Errorf(
		"background session cap reached for chat %q (max %d, currently active: %v) — "+
			"call shell_command with stop_background=\"<one of the IDs above>\" to free a slot before starting another, "+
			"or check_background to poll one of them",
		chatID, maxBackgroundSessionsPerChat, existing,
	)
}
