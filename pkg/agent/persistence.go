package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/agent_api"
)

// ConversationState represents the state of a conversation that can be persisted
type ConversationState struct {
	Messages         []api.Message `json:"messages"`
	TaskActions      []TaskAction  `json:"task_actions"`
	TotalCost        float64       `json:"total_cost"`
	TotalTokens      int           `json:"total_tokens"`
	PromptTokens     int           `json:"prompt_tokens"`
	CompletionTokens int           `json:"completion_tokens"`
	CachedTokens     int           `json:"cached_tokens"`
	CachedCostSavings float64      `json:"cached_cost_savings"`
	LastUpdated      time.Time     `json:"last_updated"`
	SessionID        string        `json:"session_id"`
}

// GetStateDir returns the directory for storing conversation state
func GetStateDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	
	stateDir := filepath.Join(homeDir, ".gpt_chat_state")
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create state directory: %w", err)
	}
	
	return stateDir, nil
}

// SaveState saves the current conversation state
func (a *Agent) SaveState(sessionID string) error {
	stateDir, err := GetStateDir()
	if err != nil {
		return err
	}
	
	state := ConversationState{
		Messages:         a.messages,
		TaskActions:      a.taskActions,
		TotalCost:        a.totalCost,
		TotalTokens:      a.totalTokens,
		PromptTokens:     a.promptTokens,
		CompletionTokens: a.completionTokens,
		CachedTokens:     a.cachedTokens,
		CachedCostSavings: a.cachedCostSavings,
		LastUpdated:      time.Now(),
		SessionID:        sessionID,
	}
	
	stateFile := filepath.Join(stateDir, fmt.Sprintf("session_%s.json", sessionID))
	
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}
	
	return os.WriteFile(stateFile, data, 0600)
}

// LoadState loads a conversation state by session ID
func (a *Agent) LoadState(sessionID string) (*ConversationState, error) {
	stateDir, err := GetStateDir()
	if err != nil {
		return nil, err
	}
	
	// Ensure the session ID doesn't already contain "session_" prefix to prevent duplication
	cleanSessionID := sessionID
	if strings.HasPrefix(sessionID, "session_") {
		// Remove the "session_" prefix if it's already there
		cleanSessionID = strings.TrimPrefix(sessionID, "session_")
	}
	
	stateFile := filepath.Join(stateDir, fmt.Sprintf("session_%s.json", cleanSessionID))
	
	data, err := os.ReadFile(stateFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}
	
	var state ConversationState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal state: %w", err)
	}
	
	return &state, nil
}

// ListSessionsWithTimestamps returns all available session IDs with their last updated timestamps
func ListSessionsWithTimestamps() ([]SessionInfo, error) {
	stateDir, err := GetStateDir()
	if err != nil {
		return nil, err
	}
	
	files, err := os.ReadDir(stateDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read state directory: %w", err)
	}
	
	var sessions []SessionInfo
	for _, file := range files {
		if !file.IsDir() && filepath.Ext(file.Name()) == ".json" {
			sessionID := file.Name()[:len(file.Name())-5] // Remove .json extension
			
			// Get file info for timestamp
			fileInfo, err := file.Info()
			if err != nil {
				continue
			}
			
			// Try to read the session file to get the last updated time from metadata
			stateFile := filepath.Join(stateDir, file.Name())
			lastUpdated := fileInfo.ModTime()
			
			// Read the file to get the actual last updated time from the state
			if data, err := os.ReadFile(stateFile); err == nil {
				var state ConversationState
				if err := json.Unmarshal(data, &state); err == nil {
					if !state.LastUpdated.IsZero() {
						lastUpdated = state.LastUpdated
					}
				}
			}
			
			sessions = append(sessions, SessionInfo{
				SessionID:   sessionID,
				LastUpdated: lastUpdated,
			})
		}
	}
	
	return sessions, nil
}

// SessionInfo represents session information with timestamp
type SessionInfo struct {
	SessionID   string    `json:"session_id"`
	LastUpdated time.Time `json:"last_updated"`
}

// ListSessions returns all available session IDs
func ListSessions() ([]string, error) {
	sessions, err := ListSessionsWithTimestamps()
	if err != nil {
		return nil, err
	}
	
	var sessionIDs []string
	for _, session := range sessions {
		sessionIDs = append(sessionIDs, session.SessionID)
	}
	
	return sessionIDs, nil
}

// DeleteSession removes a session state file
func DeleteSession(sessionID string) error {
	stateDir, err := GetStateDir()
	if err != nil {
		return err
	}
	
	// Ensure the session ID doesn't already contain "session_" prefix to prevent duplication
	cleanSessionID := sessionID
	if strings.HasPrefix(sessionID, "session_") {
		// Remove the "session_" prefix if it's already there
		cleanSessionID = strings.TrimPrefix(sessionID, "session_")
	}
	
	stateFile := filepath.Join(stateDir, fmt.Sprintf("session_%s.json", cleanSessionID))
	return os.Remove(stateFile)
}

// GenerateSessionSummary creates a summary of previous actions for continuity
func (a *Agent) GenerateSessionSummary() string {
	if len(a.taskActions) == 0 {
		return "No previous actions recorded."
	}
	
	var summary strings.Builder
	summary.WriteString("Previous session summary:\n")
	summary.WriteString("=====================================\n")
	
	// Group actions by type
	fileCreations := 0
	fileModifications := 0
	commandsExecuted := 0
	filesRead := 0
	
	for _, action := range a.taskActions {
		switch action.Type {
		case "file_created":
			fileCreations++
		case "file_modified":
			fileModifications++
		case "command_executed":
			commandsExecuted++
		case "file_read":
			filesRead++
		}
	}
	
	summary.WriteString(fmt.Sprintf("• Files created: %d\n", fileCreations))
	summary.WriteString(fmt.Sprintf("• Files modified: %d\n", fileModifications))
	summary.WriteString(fmt.Sprintf("• Commands executed: %d\n", commandsExecuted))
	summary.WriteString(fmt.Sprintf("• Files read: %d\n", filesRead))
	summary.WriteString(fmt.Sprintf("• Total cost: $%.6f\n", a.totalCost))
	summary.WriteString(fmt.Sprintf("• Total tokens: %s\n", a.formatTokenCount(a.totalTokens)))
	
	// Add recent notable actions
	if len(a.taskActions) > 0 {
		summary.WriteString("\nRecent actions:\n")
		recentCount := min(5, len(a.taskActions))
		for i := len(a.taskActions) - recentCount; i < len(a.taskActions); i++ {
			action := a.taskActions[i]
			summary.WriteString(fmt.Sprintf("• %s: %s\n", action.Type, action.Description))
		}
	}
	
	summary.WriteString("=====================================\n")
	
	return summary.String()
}

// ApplyState applies a loaded state to the current agent
func (a *Agent) ApplyState(state *ConversationState) {
	a.messages = state.Messages
	a.taskActions = state.TaskActions
	a.totalCost = state.TotalCost
	a.totalTokens = state.TotalTokens
	a.promptTokens = state.PromptTokens
	a.completionTokens = state.CompletionTokens
	a.cachedTokens = state.CachedTokens
	a.cachedCostSavings = state.CachedCostSavings
}

// GetLastMessages returns the last N messages for preview
func (a *Agent) GetLastMessages(n int) []api.Message {
	if len(a.messages) == 0 {
		return []api.Message{}
	}
	
	start := len(a.messages) - n
	if start < 0 {
		start = 0
	}
	
	return a.messages[start:]
}

// cleanupMemorySessions removes old sessions, keeping only the last 20
func cleanupMemorySessions() error {
	sessions, err := ListSessionsWithTimestamps()
	if err != nil {
		return fmt.Errorf("failed to list sessions: %w", err)
	}

	if len(sessions) <= 20 {
		return nil // No cleanup needed
	}

	// Sort sessions by last updated (oldest first)
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastUpdated.Before(sessions[j].LastUpdated)
	})

	// Delete oldest sessions beyond the 20 most recent
	for i := 0; i < len(sessions)-20; i++ {
		if err := DeleteSession(sessions[i].SessionID); err != nil {
			return fmt.Errorf("failed to delete session %s: %w", sessions[i].SessionID, err)
		}
	}

	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}