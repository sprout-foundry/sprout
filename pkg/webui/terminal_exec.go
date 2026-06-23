//go:build !js

package webui

import (
	"fmt"
	"strings"
	"time"
)

// ExecuteCommand executes a command in the specified session.
func (tm *TerminalManager) ExecuteCommand(sessionID, command string) error {
	session, exists := tm.GetSession(sessionID)
	if !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}

	session.mutex.Lock()
	defer session.mutex.Unlock()

	if !session.Active {
		return fmt.Errorf("session %s is not active", sessionID)
	}
	if session.Hidden {
		return fmt.Errorf("session %s is not accessible", sessionID)
	}

	// Add to history (without the trailing enter key) for normal commands only.
	cleanCommand := strings.TrimRight(command, "\r\n")
	controlOnly := isControlOnlyCommand(cleanCommand)
	if !controlOnly {
		// Trim whitespace and skip empty commands.
		trimmedCommand := strings.TrimSpace(cleanCommand)
		if trimmedCommand != "" {
			// Avoid consecutive duplicates.
			if len(session.History) == 0 || session.History[len(session.History)-1] != trimmedCommand {
				session.History = append(session.History, trimmedCommand)
				if len(session.History) > 1000 {
					session.History = session.History[1:]
				}
			}
			session.HistoryIndex = len(session.History)
		}
	}

	// PTY terminals expect carriage return for Enter. Control-only input
	// (for example Ctrl+C) should be forwarded as-is.
	if !controlOnly && !strings.HasSuffix(command, "\r") {
		command += "\r"
	}

	// Write command to PTY
	if session.Pty != nil {
		n, err := session.Pty.Write([]byte(command))
		if err != nil {
			return fmt.Errorf("failed to write command to PTY: %w", err)
		}
		if n == 0 {
			return fmt.Errorf("failed to write command to PTY: wrote 0 bytes")
		}
	} else {
		// Fallback for systems without PTY
		return fmt.Errorf("no PTY available for session %s", sessionID)
	}

	session.LastUsed = time.Now()
	return nil
}

// WriteRawInput writes raw terminal input bytes directly to PTY without
// command history mutation or implicit carriage-return handling.
func (tm *TerminalManager) WriteRawInput(sessionID, input string) error {
	session, exists := tm.GetSession(sessionID)
	if !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}

	session.mutex.Lock()
	defer session.mutex.Unlock()

	if !session.Active {
		return fmt.Errorf("session %s is not active", sessionID)
	}
	if session.Hidden {
		return fmt.Errorf("session %s is not accessible", sessionID)
	}

	if session.Pty == nil {
		return fmt.Errorf("no PTY available for session %s", sessionID)
	}

	if _, err := session.Pty.Write([]byte(input)); err != nil {
		return fmt.Errorf("failed to write raw input to PTY: %w", err)
	}

	session.LastUsed = time.Now()
	return nil
}

// isControlOnlyCommand returns true if the command contains only control characters.
func isControlOnlyCommand(command string) bool {
	if command == "" {
		return false
	}
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return false
	}
	for _, ch := range trimmed {
		if ch >= 32 {
			return false
		}
	}
	return true
}
