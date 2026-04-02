package webui

import (
	"fmt"
	"strings"
)

// AddToHistory adds a command to the session history.
func (tm *TerminalManager) AddToHistory(sessionID, command string) error {
	session, exists := tm.GetSession(sessionID)
	if !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}

	session.mutex.Lock()
	defer session.mutex.Unlock()

	// Trim whitespace and skip empty commands
	command = strings.TrimSpace(command)
	if command == "" {
		return nil
	}

	// Avoid duplicates
	if len(session.History) > 0 && session.History[len(session.History)-1] == command {
		return nil
	}

	// Add to history (limit to 1000 commands)
	session.History = append(session.History, command)
	if len(session.History) > 1000 {
		session.History = session.History[1:]
	}

	// Reset history index to end
	session.HistoryIndex = len(session.History)

	return nil
}

// GetHistory returns the command history for a session.
func (tm *TerminalManager) GetHistory(sessionID string) ([]string, error) {
	session, exists := tm.GetSession(sessionID)
	if !exists {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}

	session.mutex.RLock()
	defer session.mutex.RUnlock()

	// Return a copy to prevent external modification
	history := make([]string, len(session.History))
	copy(history, session.History)
	return history, nil
}

// NavigateHistory navigates through command history.
func (tm *TerminalManager) NavigateHistory(sessionID string, direction string) (string, error) {
	session, exists := tm.GetSession(sessionID)
	if !exists {
		return "", fmt.Errorf("session %s not found", sessionID)
	}

	session.mutex.Lock()
	defer session.mutex.Unlock()

	if len(session.History) == 0 {
		return "", nil
	}

	switch direction {
	case "up":
		if session.HistoryIndex > 0 {
			session.HistoryIndex--
		}
	case "down":
		if session.HistoryIndex < len(session.History)-1 {
			session.HistoryIndex++
		} else if session.HistoryIndex == len(session.History)-1 {
			// If we're at the last command and go down, return empty string
			session.HistoryIndex = len(session.History)
			return "", nil
		}
	default:
		return "", fmt.Errorf("invalid direction: %s", direction)
	}

	if session.HistoryIndex < len(session.History) {
		return session.History[session.HistoryIndex], nil
	}
	return "", nil
}

// ResetHistoryIndex resets the history index to the end (for new input).
func (tm *TerminalManager) ResetHistoryIndex(sessionID string) error {
	session, exists := tm.GetSession(sessionID)
	if !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}

	session.mutex.Lock()
	defer session.mutex.Unlock()

	session.HistoryIndex = len(session.History)
	return nil
}
