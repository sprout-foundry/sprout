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

// ExecuteSessionList lists sessions for the current working directory, newest first.
func (f *SessionsFlow) ExecuteSessionList(chatAgent *agent.Agent) (string, error) {
	sessions, err := agent.ListSessionsWithTimestamps()
	if err != nil {
		return "", fmt.Errorf("failed to list sessions: %w", err)
	}

	if len(sessions) == 0 {
		return "No saved sessions found.", nil
	}

	var builder strings.Builder
	builder.WriteString("[list] Saved Sessions (current directory, newest first):\n")
	builder.WriteString(strings.Repeat("-", 100))
	builder.WriteString("\n")

	for i, session := range sessions {
		// Build label with name if available, otherwise get preview
		name := session.Name
		preview := ""
		if name == "" {
			preview = agent.GetSessionPreviewScoped(session.SessionID, session.WorkingDirectory)
		}

		// Format: #ID  [Name] Timestamp  SessionID
		timeStr := session.LastUpdated.Format(time.RFC3339)
		if name != "" {
			builder.WriteString(fmt.Sprintf("#%d  [%s]\n", i+1, name))
		} else if preview != "" {
			builder.WriteString(fmt.Sprintf("#%d  [%s]\n", i+1, preview))
		} else {
			builder.WriteString(fmt.Sprintf("#%d  [Unnamed session]\n", i+1))
		}
		builder.WriteString(fmt.Sprintf("     Time: %s | ID: %s\n", timeStr, session.SessionID))

		// Display working directory for easier identification
		if session.WorkingDirectory != "" {
			builder.WriteString(fmt.Sprintf("     Dir:  %s\n", session.WorkingDirectory))
		}
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
		return "", fmt.Errorf("failed to list sessions: %w", err)
	}

	// Find session by number or ID
	var selectedSession *agent.SessionInfo
	if len(args) > 0 && args[0] != "--full" {
		// Try to parse as number
		num := 0
		if _, err := fmt.Sscanf(args[0], "%d", &num); err == nil && num >= 1 && num <= len(sessions) {
			selectedSession = &sessions[num-1]
		} else {
			// Try to find by ID prefix
			for i := range sessions {
				s := sessions[i]
				if strings.HasPrefix(s.SessionID, args[0]) {
					selectedSession = &sessions[i]
					break
				}
			}
			if selectedSession == nil {
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
	state, err := agent.LoadStateWithoutAgentScoped(selectedSession.SessionID, selectedSession.WorkingDirectory)
	if err != nil {
		return "", fmt.Errorf("failed to load session: %w", err)
	}

	chatAgent.ApplyState(state)

	confirm := "summary only (for full context reload, add --full flag)."
	if fullLoad {
		confirm = "full context."
	}

	return fmt.Sprintf("[OK] Session loaded (%s)", confirm), nil
}

// ExecuteSessionRename renames a session
func (f *SessionsFlow) ExecuteSessionRename(chatAgent *agent.Agent, args []string) (string, error) {
	if len(args) < 2 {
		return "Usage: /sessions rename <number|id> <new_name>\nExample: /sessions rename 1 \"Refactor persistence module\"", nil
	}

	sessions, err := agent.ListSessionsWithTimestamps()
	if err != nil {
		return "", fmt.Errorf("failed to list sessions: %w", err)
	}

	// Find session by number or ID
	var selectedSession *agent.SessionInfo
	num := 0
	if _, err := fmt.Sscanf(args[0], "%d", &num); err == nil && num >= 1 && num <= len(sessions) {
		selectedSession = &sessions[num-1]
	} else {
		// Try to find by ID prefix
		for i := range sessions {
			s := sessions[i]
			if strings.HasPrefix(s.SessionID, args[0]) {
				selectedSession = &sessions[i]
				break
			}
		}
		if selectedSession == nil {
			return "", fmt.Errorf("session number or ID not found: %s", args[0])
		}
	}

	// Get new name (combine remaining args)
	newName := strings.Join(args[1:], " ")

	// Load the session state, update name, and save it
	if err := agent.RenameSessionScoped(selectedSession.SessionID, newName, selectedSession.WorkingDirectory); err != nil {
		return "", fmt.Errorf("failed to rename session: %w", err)
	}

	return fmt.Sprintf("[OK] Session renamed to: %s", newName), nil
}

// ExecuteSessionDelete removes a session file
func (f *SessionsFlow) ExecuteSessionDelete(args []string) (string, error) {
	if len(args) == 0 {
		return "Usage: /sessions delete <number|id>\nExample: /sessions delete 1 (or /sessions delete session_1734697500)", nil
	}

	sessions, err := agent.ListSessionsWithTimestamps()
	if err != nil {
		return "", fmt.Errorf("failed to list sessions: %w", err)
	}

	// Find session by number or ID
	var selectedSession *agent.SessionInfo
	num := 0
	if _, err := fmt.Sscanf(args[0], "%d", &num); err == nil && num >= 1 && num <= len(sessions) {
		selectedSession = &sessions[num-1]
	} else {
		// Try to find by ID
		for i := range sessions {
			s := sessions[i]
			if s.SessionID == args[0] || strings.HasPrefix(s.SessionID, args[0]) {
				selectedSession = &sessions[i]
				break
			}
		}
		if selectedSession == nil {
			return "", fmt.Errorf("session number or ID not found: %s", args[0])
		}
	}

	// Delete the session file
	if err := agent.DeleteSessionScoped(selectedSession.SessionID, selectedSession.WorkingDirectory); err != nil {
		return "", fmt.Errorf("failed to delete session: %w", err)
	}

	return fmt.Sprintf("[OK] Session deleted: %s", selectedSession.SessionID), nil
}

// ExecuteSessionExport exports a session to a JSON file
func (f *SessionsFlow) ExecuteSessionExport(args []string) (string, error) {
	if len(args) < 2 {
		return "Usage: /sessions export <number|id> <output_file.json>\nExample: /sessions export 1 my_session.json", nil
	}

	sessions, err := agent.ListSessionsWithTimestamps()
	if err != nil {
		return "", fmt.Errorf("failed to list sessions: %w", err)
	}

	// Find session by number or ID
	var selectedSession *agent.SessionInfo
	num := 0
	if _, err := fmt.Sscanf(args[0], "%d", &num); err == nil && num >= 1 && num <= len(sessions) {
		selectedSession = &sessions[num-1]
	} else {
		// Try to find by ID
		for i := range sessions {
			s := sessions[i]
			if s.SessionID == args[0] || strings.HasPrefix(s.SessionID, args[0]) {
				selectedSession = &sessions[i]
				break
			}
		}
		if selectedSession == nil {
			return "", fmt.Errorf("session number or ID not found: %s", args[0])
		}
	}

	// Load state and export to file
	state, err := agent.LoadStateWithoutAgentScoped(selectedSession.SessionID, selectedSession.WorkingDirectory)
	if err != nil {
		return "", fmt.Errorf("failed to load session for export: %w", err)
	}

	// Write to file using os.WriteFile
	filename := args[1]
	data, err := agent.ExportStateToJSON(state)
	if err != nil {
		return "", fmt.Errorf("failed to export session state: %w", err)
	}

	if err := os.WriteFile(filename, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write export file: %w", err)
	}

	return fmt.Sprintf("[OK] Session exported to: %s", filename), nil
}

// ExecuteSessionImport imports a session from a JSON file
func (f *SessionsFlow) ExecuteSessionImport(chatAgent *agent.Agent, args []string) (string, error) {
	if len(args) == 0 {
		return "Usage: /sessions import <file.json>\nExample: /sessions import my_session.json", nil
	}

	filename := args[0]
	state, err := agent.ImportStateFromJSONFile(filename)
	if err != nil {
		return "", fmt.Errorf("failed to import session: %w", err)
	}

	chatAgent.ApplyState(state)
	return fmt.Sprintf("[OK] Session imported from: %s", filename), nil
}
