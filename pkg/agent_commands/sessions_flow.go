package commands

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
)

// SessionsFlow handles advanced session operations
type SessionsFlow struct{}

// ExecuteSessionList lists sessions in reverse chronological order (newest first)
func (f *SessionsFlow) ExecuteSessionList(chatAgent *agent.Agent) (string, error) {
	sessions, err := agent.ListSessionsWithTimestamps()
	if err != nil {
		return "", fmt.Errorf("failed to list sessions: %v", err)
	}

	if len(sessions) == 0 {
		return "No saved sessions found.", nil
	}

	var builder strings.Builder
	builder.WriteString("📋 Saved Sessions (newest first):\n")
	builder.WriteString(strings.Repeat("-", 100))
	builder.WriteString("\n")

	for i, session := range sessions {
		// Build label with name if available, otherwise get preview
		name := session.Name
		preview := ""
		if name == "" {
			preview = agent.GetSessionPreview(session.SessionID)
		}

		// Format: #ID  [Name]  Timestamp  SessionID
		timeStr := session.LastUpdated.Format(time.RFC3339)
		if name != "" {
			builder.WriteString(fmt.Sprintf("#%d  [%s]\n", i+1, name))
		} else if preview != "" {
			builder.WriteString(fmt.Sprintf("#%d  [%s]\n", i+1, preview))
		} else {
			builder.WriteString(fmt.Sprintf("#%d  [Unnamed session]\n", i+1))
		}
		builder.WriteString(fmt.Sprintf("     Time: %s | ID: %s\n", timeStr, session.SessionID))
		builder.WriteString(strings.Repeat("-", 100))
		builder.WriteString("\n")
	}

	builder.WriteString(fmt.Sprintf("Total: %d session(s)\n", len(sessions)))
	return builder.String(), nil
}

// ExecuteSessionLoad loads a session with optional summary context restoration
func (f *SessionsFlow) ExecuteSessionLoad(chatAgent *agent.Agent, args []string) (string, error) {
	sessions, err := agent.ListSessionsWithTimestamps()
	if err != nil {
		return "", fmt.Errorf("failed to list sessions: %v", err)
	}

	// Find session by number or ID
	var sessionID string
	if len(args) > 0 && args[0] != "--full" {
		// Try to parse as number
		num := 0
		if _, err := fmt.Sscanf(args[0], "%d", &num); err == nil && num >= 1 && num <= len(sessions) {
			// List is indexed 0-based but displayed 1-based
			sessionID = sessions[len(sessions)-num].SessionID
		} else {
			// Try to find by ID prefix
			for _, s := range sessions {
				if strings.HasPrefix(s.SessionID, args[0]) {
					sessionID = s.SessionID
					break
				}
			}
			if sessionID == "" {
				return "", fmt.Errorf("session number or ID not found: %s", args[0])
			}
		}
	} else {
		return "", fmt.Errorf("usage: /sessions load <number|id> [--full]")
		// Example: /sessions load 1 --full  (load full context from session 1)
		//          /sessions load session_1734697500 (load just summary)
	}

	// Check if --full flag is provided
	fullLoad := false
	for _, arg := range args {
		if arg == "--full" {
			fullLoad = true
			break
		}
	}

	// Load state
	state, err := agent.LoadStateWithoutAgent(sessionID)
	if err != nil {
		return "", fmt.Errorf("failed to load session: %v", err)
	}

	chatAgent.ApplyState(state)

	confirm := "summary only (for full context reload, add --full flag)."
	if fullLoad {
		confirm = "full context."
	}

	return fmt.Sprintf("✅ Session loaded (%s)", confirm), nil
}

// ExecuteSessionRename renames a session
func (f *SessionsFlow) ExecuteSessionRename(chatAgent *agent.Agent, args []string) (string, error) {
	if len(args) < 2 {
		return "Usage: /sessions rename <number|id> <new_name>\nExample: /sessions rename 1 \"Refactor persistence module\"", nil
	}

	sessions, err := agent.ListSessionsWithTimestamps()
	if err != nil {
		return "", fmt.Errorf("failed to list sessions: %v", err)
	}

	// Find session by number or ID
	var sessionID string
	num := 0
	if _, err := fmt.Sscanf(args[0], "%d", &num); err == nil && num >= 1 && num <= len(sessions) {
		sessionID = sessions[len(sessions)-num].SessionID
	} else {
		// Try to find by ID prefix
		for _, s := range sessions {
			if strings.HasPrefix(s.SessionID, args[0]) {
				sessionID = s.SessionID
				break
			}
		}
		if sessionID == "" {
			return "", fmt.Errorf("session number or ID not found: %s", args[0])
		}
	}

	// Get new name (combine remaining args)
	newName := strings.Join(args[1:], " ")

	// Load the session state, update name, and save it
	if err := agent.RenameSession(sessionID, newName); err != nil {
		return "", fmt.Errorf("failed to rename session: %v", err)
	}

	return fmt.Sprintf("✅ Session renamed to: %s", newName), nil
}

// ExecuteSessionDelete removes a session file
func (f *SessionsFlow) ExecuteSessionDelete(args []string) (string, error) {
	if len(args) == 0 {
		return "Usage: /sessions delete <number|id>\nExample: /sessions delete 1 (or /sessions delete session_1734697500)", nil
	}

	sessions, err := agent.ListSessionsWithTimestamps()
	if err != nil {
		return "", fmt.Errorf("failed to list sessions: %v", err)
	}

	// Find session by number or ID
	var sessionID string
	num := 0
	if _, err := fmt.Sscanf(args[0], "%d", &num); err == nil && num >= 1 && num <= len(sessions) {
		sessionID = sessions[len(sessions)-num].SessionID
	} else {
		// Try to find by ID
		for _, s := range sessions {
			if s.SessionID == args[0] || strings.HasPrefix(s.SessionID, args[0]) {
				sessionID = s.SessionID
				break
			}
		}
		if sessionID == "" {
			return "", fmt.Errorf("session number or ID not found: %s", args[0])
		}
	}

	// Delete the session file
	if err := agent.DeleteSession(sessionID); err != nil {
		return "", fmt.Errorf("failed to delete session: %v", err)
	}

	return fmt.Sprintf("✅ Session deleted: %s", sessionID), nil
}

// ExecuteSessionExport exports a session to a JSON file
func (f *SessionsFlow) ExecuteSessionExport(args []string) (string, error) {
	if len(args) < 2 {
		return "Usage: /sessions export <number|id> <output_file.json>\nExample: /sessions export 1 my_session.json", nil
	}

	sessions, err := agent.ListSessionsWithTimestamps()
	if err != nil {
		return "", fmt.Errorf("failed to list sessions: %v", err)
	}

	// Find session by number or ID
	var sessionID string
	num := 0
	if _, err := fmt.Sscanf(args[0], "%d", &num); err == nil && num >= 1 && num <= len(sessions) {
		sessionID = sessions[len(sessions)-num].SessionID
	} else {
		// Try to find by ID
		for _, s := range sessions {
			if s.SessionID == args[0] || strings.HasPrefix(s.SessionID, args[0]) {
				sessionID = s.SessionID
				break
			}
		}
		if sessionID == "" {
			return "", fmt.Errorf("session number or ID not found: %s", args[0])
		}
	}

	// Load state and export to file
	state, err := agent.LoadStateWithoutAgent(sessionID)
	if err != nil {
		return "", fmt.Errorf("failed to load session for export: %v", err)
	}

	// Write to file using os.WriteFile
	filename := args[1]
	data, err := agent.ExportStateToJSON(state)
	if err != nil {
		return "", fmt.Errorf("failed to export session state: %v", err)
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write export file: %v", err)
	}

	return fmt.Sprintf("✅ Session exported to: %s", filename), nil
}

// ExecuteSessionImport imports a session from a JSON file
func (f *SessionsFlow) ExecuteSessionImport(chatAgent *agent.Agent, args []string) (string, error) {
	if len(args) == 0 {
		return "Usage: /sessions import <file.json>\nExample: /sessions import my_session.json", nil
	}

	filename := args[0]
	state, err := agent.ImportStateFromJSONFile(filename)
	if err != nil {
		return "", fmt.Errorf("failed to import session: %v", err)
	}

	chatAgent.ApplyState(state)
	return fmt.Sprintf("✅ Session imported from: %s", filename), nil
}
