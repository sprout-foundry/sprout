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

// Reset to default when running tests (helps with parallel test safety)
func init() {
	getStateDirFunc = defaultGetStateDir
}

// ConversationState represents the state of a conversation that can be persisted
type ConversationState struct {
	Messages          []api.Message `json:"messages"`
	TaskActions       []TaskAction  `json:"task_actions"`
	TotalCost         float64       `json:"total_cost"`
	TotalTokens       int           `json:"total_tokens"`
	PromptTokens      int           `json:"prompt_tokens"`
	CompletionTokens  int           `json:"completion_tokens"`
	CachedTokens      int           `json:"cached_tokens"`
	CachedCostSavings float64       `json:"cached_cost_savings"`
	LastUpdated       time.Time     `json:"last_updated"`
	SessionID         string        `json:"session_id"`
	Name              string        `json:"name"` // Human-readable session name
}

// Variable to allow overriding GetStateDir for testing
var getStateDirFunc = defaultGetStateDir

// GetStateDir returns the directory for storing conversation state
func GetStateDir() (string, error) {
	return getStateDirFunc()
}

// defaultGetStateDir is the actual implementation of GetStateDir
func defaultGetStateDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	stateDir := filepath.Join(homeDir, ".ledit", "sessions")
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

	// Generate session name from first user message
	sessionName := a.generateSessionName()

	state := ConversationState{
		Messages:          a.messages,
		TaskActions:       a.taskActions,
		TotalCost:         a.totalCost,
		TotalTokens:       a.totalTokens,
		PromptTokens:      a.promptTokens,
		CompletionTokens:  a.completionTokens,
		CachedTokens:      a.cachedTokens,
		CachedCostSavings: a.cachedCostSavings,
		LastUpdated:       time.Now(),
		SessionID:         sessionID,
		Name:              sessionName,
	}

	stateFile := filepath.Join(stateDir, fmt.Sprintf("session_%s.json", sessionID))

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	return os.WriteFile(stateFile, data, 0600)
}

// LoadStateWithoutAgent loads a conversation state by session ID without an Agent instance
func LoadStateWithoutAgent(sessionID string) (*ConversationState, error) {
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

// LoadState loads a conversation state by session ID
func (a *Agent) LoadState(sessionID string) (*ConversationState, error) {
	return LoadStateWithoutAgent(sessionID)
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

			// Try to read the session file to get the last updated time and name from state
			stateFile := filepath.Join(stateDir, file.Name())
			lastUpdated := fileInfo.ModTime()
			name := ""

			// Read the file to get the actual last updated time and name from the state
			if data, err := os.ReadFile(stateFile); err == nil {
				var state ConversationState
				if err := json.Unmarshal(data, &state); err == nil {
					if !state.LastUpdated.IsZero() {
						lastUpdated = state.LastUpdated
					}
					name = state.Name
				}
			}

			sessions = append(sessions, SessionInfo{
				SessionID:   sessionID,
				LastUpdated: lastUpdated,
				Name:        name,
			})
		}
	}

	return sessions, nil
}

// SessionInfo represents session information with timestamp
type SessionInfo struct {
	SessionID   string    `json:"session_id"`
	LastUpdated time.Time `json:"last_updated"`
	Name        string    `json:"name"` // Human-readable session name
}

// GetSessionPreview returns the first 50 characters of the first user message
func GetSessionPreview(sessionID string) string {
	stateDir, err := GetStateDir()
	if err != nil {
		return ""
	}

	// Ensure the session ID doesn't already contain "session_" prefix to prevent duplication
	cleanSessionID := sessionID
	if strings.HasPrefix(sessionID, "session_") {
		cleanSessionID = strings.TrimPrefix(sessionID, "session_")
	}

	stateFile := filepath.Join(stateDir, fmt.Sprintf("session_%s.json", cleanSessionID))
	data, err := os.ReadFile(stateFile)
	if err != nil {
		return ""
	}

	var state ConversationState
	if err := json.Unmarshal(data, &state); err != nil {
		return ""
	}

	// Find the first user message
	for _, msg := range state.Messages {
		if msg.Role == "user" && strings.TrimSpace(msg.Content) != "" {
			// Get first 50 characters, clean up whitespace
			content := strings.TrimSpace(msg.Content)
			if len(content) > 50 {
				content = content[:50] + "..."
			}
			// Replace newlines with spaces to keep it on one line
			content = strings.ReplaceAll(content, "\n", " ")
			return content
		}
	}

	return ""
}

// GetSessionName returns the name of a session
func GetSessionName(sessionID string) string {
	stateDir, err := GetStateDir()
	if err != nil {
		return ""
	}

	// Ensure the session ID doesn't already contain "session_" prefix to prevent duplication
	cleanSessionID := sessionID
	if strings.HasPrefix(sessionID, "session_") {
		cleanSessionID = strings.TrimPrefix(sessionID, "session_")
	}

	stateFile := filepath.Join(stateDir, fmt.Sprintf("session_%s.json", cleanSessionID))
	data, err := os.ReadFile(stateFile)
	if err != nil {
		return ""
	}

	var state ConversationState
	if err := json.Unmarshal(data, &state); err != nil {
		return ""
	}

	return state.Name
}

// RenameSession renames a session by updating the name field in the state file
func RenameSession(sessionID string, newName string) error {
	stateDir, err := GetStateDir()
	if err != nil {
		return err
	}

	// Ensure the session ID doesn't already contain "session_" prefix to prevent duplication
	cleanSessionID := sessionID
	if strings.HasPrefix(sessionID, "session_") {
		cleanSessionID = strings.TrimPrefix(sessionID, "session_")
	}

	if strings.HasPrefix(cleanSessionID, "session_") {
		cleanSessionID = strings.TrimPrefix(cleanSessionID, "session_")
	}

	stateFile := filepath.Join(stateDir, fmt.Sprintf("session_%s.json", cleanSessionID))

	data, err := os.ReadFile(stateFile)
	if err != nil {
		return fmt.Errorf("failed to read session file: %w", err)
	}

	var state ConversationState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("failed to unmarshal state: %w", err)
	}

	// Update the name
	state.Name = newName

	// Write back to file
 newData, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	if err := os.WriteFile(stateFile, newData, 0600); err != nil {
		return fmt.Errorf("failed to write session file: %w", err)
	}

	return nil
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
	// Apply saved state
	a.messages = state.Messages
	a.taskActions = state.TaskActions
	a.totalCost = state.TotalCost
	a.totalTokens = state.TotalTokens
	a.promptTokens = state.PromptTokens
	a.completionTokens = state.CompletionTokens
	a.cachedTokens = state.CachedTokens
	a.cachedCostSavings = state.CachedCostSavings

	// CRITICAL: Reset session state to prevent hanging issues after session restore
	a.currentIteration = 0
	a.contextWarningIssued = false

	// Reset circuit breaker state to prevent false positives
	if a.circuitBreaker != nil {
		a.circuitBreaker.mu.Lock()
		// Clear entries instead of replacing map to avoid memory churn and reduce lock hold time
		for key := range a.circuitBreaker.Actions {
			delete(a.circuitBreaker.Actions, key)
		}
		a.circuitBreaker.mu.Unlock()
	}

	// Clear streaming buffer to prevent old content from interfering
	a.streamingBuffer.Reset()

	// Reset shell command history to prevent stale cache issues
	if a.shellCommandHistory == nil {
		a.shellCommandHistory = make(map[string]*ShellCommandResult)
	} else {
		// Clear existing history
		for k := range a.shellCommandHistory {
			delete(a.shellCommandHistory, k)
		}
	}
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

// ExportStateToJSON converts a ConversationState to JSON bytes
func ExportStateToJSON(state *ConversationState) ([]byte, error) {
	return json.MarshalIndent(state, "", "  ")
}

// ImportStateFromJSONFile loads a ConversationState from a JSON file
func ImportStateFromJSONFile(filename string) (*ConversationState, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read import file: %w", err)
	}

	var state ConversationState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal state from file: %w", err)
	}

	return &state, nil
}
