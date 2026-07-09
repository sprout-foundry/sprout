package agent

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// ExportState exports the current agent state for persistence
func (a *Agent) ExportState() ([]byte, error) {
	// Generate compact summary for next session continuity
	compactSummary := a.GenerateCompactSummary()
	taskActions := a.GetTaskActions()

	state := AgentState{
		Messages:                a.state.GetMessages(),
		MessageTimestamps:       a.state.GetMessageTimestamps(),
		TurnCheckpoints:         a.copyTurnCheckpoints(),
		PreviousSummary:         a.state.GetPreviousSummary(),
		CompactSummary:          compactSummary, // Store 5K-limited summary for continuity
		TaskActions:             taskActions,
		SessionID:               a.state.GetSessionID(),
		TotalTokens:             a.state.GetTotalTokens(),
		TotalCost:               a.state.GetTotalCost(),
		PromptTokens:            a.state.GetPromptTokens(),
		CompletionTokens:        a.state.GetCompletionTokens(),
		EstimatedTokenResponses: a.state.GetEstimatedTokenResponses(),
		CachedTokens:            a.state.GetCachedTokens(),
		CacheWriteTokens:        a.state.GetCacheWriteTokens(),
		CachedCostSavings:       a.state.GetCachedCostSavings(),
		ChargedCostTotal:        a.state.GetChargedCostTotal(),
		TokenCostTotal:          a.state.GetTokenCostTotal(),
		SubscriptionTokens:      a.state.GetSubscriptionTokens(),
		FreeTokens:              a.state.GetFreeTokens(),
	}
	return json.Marshal(state)
}

// ImportState imports agent state from JSON data
func (a *Agent) ImportState(data []byte) error {
	var state AgentState
	if err := json.Unmarshal(data, &state); err != nil {
		return agenterrors.NewAgent("state", "failed to import state", err)
	}
	a.state.SetMessages(state.Messages)
	a.state.SetMessageTimestamps(state.MessageTimestamps)
	a.ReplaceTurnCheckpoints(state.TurnCheckpoints)
	// Prefer compact summary for continuity, fallback to legacy summary
	if state.CompactSummary != "" {
		a.state.SetPreviousSummary(state.CompactSummary)
	} else {
		a.state.SetPreviousSummary(state.PreviousSummary)
	}
	a.replaceTaskActions(state.TaskActions)
	a.state.SetSessionID(state.SessionID)
	// Restore metrics
	a.state.SetTotalTokens(state.TotalTokens)
	a.state.SetTotalCost(state.TotalCost)
	a.state.SetPromptTokens(state.PromptTokens)
	a.state.SetCompletionTokens(state.CompletionTokens)
	a.state.SetEstimatedTokenResponses(state.EstimatedTokenResponses)
	a.state.SetCachedTokens(state.CachedTokens)
	a.state.SetCacheWriteTokens(state.CacheWriteTokens)
	a.state.SetCachedCostSavings(state.CachedCostSavings)
	a.state.SetChargedCostTotal(state.ChargedCostTotal)
	a.state.SetTokenCostTotal(state.TokenCostTotal)
	a.state.SetSubscriptionTokens(state.SubscriptionTokens)
	a.state.SetFreeTokens(state.FreeTokens)
	return nil
}

func (a *Agent) replaceTaskActions(actions []TaskAction) {
	cloned := make([]TaskAction, len(actions))
	copy(cloned, actions)

	taskActionsMu := a.state.GetTaskActionsMutex()
	taskActionsMu.Lock()
	a.state.SetTaskActions(cloned)
	taskActionsMu.Unlock()
}

// validateStateFilePath validates that a filename is safe for state file operations.
// It prevents:
//  1. Absolute path writes (e.g., /etc/passwd)
//  2. Path traversal via ".." components (e.g., ../../etc/passwd)
//  3. Null bytes in filenames (cross-platform consistency)
//  4. Symlinks that could redirect writes to arbitrary files
//
// Only simple filenames or safe relative paths within the current directory are allowed.
func validateStateFilePath(filename string) (string, error) {
	trimmed := strings.TrimSpace(filename)
	if trimmed == "" {
		return "", agenterrors.NewValidation("state file path cannot be empty", nil)
	}

	// Reject null bytes (valid on some filesystems but confusing and dangerous)
	if strings.Contains(trimmed, "\x00") {
		return "", agenterrors.NewValidation(fmt.Sprintf("state file path %q contains invalid null byte", filename), nil)
	}

	// Clean the path to resolve any "." or ".." segments
	cleaned := filepath.Clean(trimmed)

	// Reject absolute paths
	if filepath.IsAbs(cleaned) {
		return "", agenterrors.NewValidation(fmt.Sprintf("state file path %q cannot be an absolute path", filename), nil)
	}

	// Reject paths that still contain ".." after cleaning (path traversal)
	if strings.Contains(cleaned, "..") {
		return "", agenterrors.NewValidation(fmt.Sprintf("state file path %q contains invalid path traversal components", filename), nil)
	}

	// Ensure path doesn't start with path separator (extra check for Windows compatibility)
	if strings.HasPrefix(cleaned, string(os.PathSeparator)) || strings.HasPrefix(cleaned, "/") {
		return "", agenterrors.NewValidation(fmt.Sprintf("state file path %q cannot start with path separator", filename), nil)
	}

	// Reject symlinks to prevent writes to arbitrary files outside the working directory
	if info, err := os.Lstat(cleaned); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", agenterrors.NewValidation(fmt.Sprintf("state file path %q is a symlink; symlinks are not allowed for security", filename), nil)
	}

	return cleaned, nil
}

// SaveStateToFile saves agent state to a file
func (a *Agent) SaveStateToFile(filename string) error {
	validatedPath, err := validateStateFilePath(filename)
	if err != nil {
		return err
	}

	stateData, err := a.ExportState()
	if err != nil {
		return agenterrors.NewAgent("state", "failed to export state", err)
	}
	return os.WriteFile(validatedPath, stateData, 0644)
}

// LoadStateFromFile loads agent state from a file
func (a *Agent) LoadStateFromFile(filename string) error {
	validatedPath, err := validateStateFilePath(filename)
	if err != nil {
		return err
	}

	data, err := os.ReadFile(validatedPath)
	if err != nil {
		return agenterrors.NewAgent("state", "failed to read state file", err)
	}
	return a.ImportState(data)
}

// LoadSummaryFromFile loads ONLY the compact summary from a state file for minimal continuity
func (a *Agent) LoadSummaryFromFile(filename string) error {
	validatedPath, err := validateStateFilePath(filename)
	if err != nil {
		return err
	}

	data, err := os.ReadFile(validatedPath)
	if err != nil {
		return agenterrors.NewAgent("state", "failed to read summary file", err)
	}

	var state AgentState
	if err := json.Unmarshal(data, &state); err != nil {
		return agenterrors.NewAgent("state", "failed to unmarshal summary", err)
	}

	// Only load the compact summary, not the full conversation state
	if state.CompactSummary != "" {
		a.state.SetPreviousSummary(state.CompactSummary)
		if a.debug {
			a.Logger().Debug("[doc] Loaded compact summary (%d chars)\n", len(state.CompactSummary))
		}
	} else {
		// Fallback to legacy summary if compact summary not available
		if state.PreviousSummary != "" {
			a.state.SetPreviousSummary(state.PreviousSummary)
			if a.debug {
				a.Logger().Debug("[doc] Loaded legacy summary (%d chars)\n", len(state.PreviousSummary))
			}
		}
	}

	return nil
}

// SaveConversationSummary saves the conversation summary to the state file
func (a *Agent) SaveConversationSummary() error {
	// Generate summary before saving
	_ = a.GenerateConversationSummary() // Generate summary to update state

	// Save state to file
	stateFile := ".coder_state.json"
	if err := a.SaveStateToFile(stateFile); err != nil {
		return agenterrors.NewAgent("state", "failed to save conversation state", err)
	}

	if a.debug {
		a.Logger().Debug("[save] Saved conversation summary to %s\n", stateFile)
	}

	return nil
}

// AddTaskAction records a completed task action for continuity
func (a *Agent) AddTaskAction(actionType, description, details string) {
	taskActionsMu := a.state.GetTaskActionsMutex()
	taskActionsMu.Lock()
	currentActions := a.state.GetTaskActions()
	newActions := make([]TaskAction, len(currentActions)+1)
	copy(newActions, currentActions)
	newActions[len(newActions)-1] = TaskAction{
		Type:        actionType,
		Description: description,
		Details:     details,
	}
	a.state.SetTaskActions(newActions)
	taskActionsMu.Unlock()
}

// GenerateActionSummary creates a summary of completed actions for continuity
func (a *Agent) GenerateActionSummary() string {
	taskActions := a.GetTaskActions()
	if len(taskActions) == 0 {
		return "No actions completed yet."
	}

	var summary strings.Builder
	summary.WriteString("Previous actions completed:\n")

	for i, action := range taskActions {
		summary.WriteString(fmt.Sprintf("%d. %s: %s", i+1, action.Type, action.Description))
		if action.Details != "" {
			summary.WriteString(fmt.Sprintf(" (%s)", action.Details))
		}
		summary.WriteString("\n")
	}

	return summary.String()
}

// SetPreviousSummary sets the summary of previous actions for continuity
func (a *Agent) SetPreviousSummary(summary string) {
	a.state.SetPreviousSummary(summary)
}

// GetPreviousSummary returns the summary of previous actions
func (a *Agent) GetPreviousSummary() string {
	return a.state.GetPreviousSummary()
}

// SetSessionID sets the session identifier for continuity
func (a *Agent) SetSessionID(sessionID string) {
	if a.state == nil {
		a.state = NewAgentStateManager(false)
	}
	a.state.SetSessionID(sessionID)
}

// SetSessionName explicitly sets a custom name for the current session
func (a *Agent) SetSessionName(name string) {
	// Store custom name - will be used on next save
	pattern := "[SESSION_NAME:]"
	messages := a.state.GetMessages()
	for i, msg := range messages {
		if strings.HasPrefix(msg.Content, pattern) {
			messages[i].Content = pattern + name
			a.state.SetMessages(messages)
			return
		}
	}
	a.shiftTurnCheckpoints(1)
	a.state.AddMessage(api.Message{Role: "system", Content: pattern + name})
}

// GetSessionID returns the session identifier
func (a *Agent) GetSessionID() string {
	if a.state == nil {
		return ""
	}
	return a.state.GetSessionID()
}

// autoSaveState automatically saves the current conversation state
func (a *Agent) autoSaveState() {
	// Generate session ID based on timestamp if not set
	if a.state.GetSessionID() == "" {
		a.state.SetSessionID(fmt.Sprintf("session_%d", time.Now().Unix()))
	}

	if err := a.SaveStateScoped(a.state.GetSessionID(), a.currentWorkspaceRoot()); err != nil {
		if a.debug {
			a.Logger().Debug("[WARN] Failed to write state file for auto-save: %v\n", err)
		}
		return
	}

	if a.debug {
		a.Logger().Debug("[save] Auto-saved scoped conversation state for session %s\n", a.state.GetSessionID())
	}
}

// newSessionID returns a session identifier for a freshly rotated session.
// Format: session_<unix-nano>_<6 random hex bytes> — collision-resistant
// across rapid rotations within the same nanosecond, distinct from
// autoSaveState's session_<unix-seconds> shape so rotated sessions are
// trivially distinguishable from auto-assigned ones.
func newSessionID() string {
	token := make([]byte, 6)
	if _, err := rand.Read(token); err != nil {
		// crypto/rand should not fail on a healthy system; fall back to a
		// timestamp-only ID so we never block rotation on entropy errors.
		return fmt.Sprintf("session_%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("session_%d_%s", time.Now().UnixNano(), hex.EncodeToString(token))
}

// RotateSession closes the current session as a complete, restorable unit
// (writing its final state to disk under the current SessionID), then assigns
// a new SessionID and clears in-memory conversation state. The previous
// session file remains loadable via LoadStateScoped. Returns the new session ID.
//
// If the prior session's SaveStateScoped fails (e.g. invalid session ID or
// unwritable working directory), RotateSession returns that error WITHOUT
// rotating — the prior session must remain intact so the caller can retry.
func (a *Agent) RotateSession() (string, error) {
	if a.state == nil {
		a.state = NewAgentStateManager(false)
	}

	currentID := a.state.GetSessionID()
	if currentID != "" {
		if err := a.SaveStateScoped(currentID, a.currentWorkspaceRoot()); err != nil {
			return "", agenterrors.Wrap(err, "rotate: failed to snapshot prior session")
		}
	}

	a.ClearConversationHistory()

	newID := newSessionID()
	a.SetSessionID(newID)
	return newID, nil
}

// generateSessionName generates a readable session name from first user message
func (a *Agent) generateSessionName() string {
	// First check if a custom session name is set via SetSessionName
	messages := a.state.GetMessages()
	for _, msg := range messages {
		if msg.Role == "system" && strings.HasPrefix(msg.Content, "[SESSION_NAME:]") {
			name := strings.TrimPrefix(msg.Content, "[SESSION_NAME:]")
			if strings.TrimSpace(name) != "" {
				return strings.TrimSpace(name)
			}
		}
	}
	// Otherwise derive from first user message
	for _, msg := range messages {
		if msg.Role == "user" && strings.TrimSpace(msg.Content) != "" {
			name := strings.TrimSpace(msg.Content)
			name = strings.Join(strings.Fields(name), " ")
			if len(name) > 60 {
				name = name[:60] + "..."
			}
			return name
		}
	}
	return "Unnamed session"
}

// GetSessionName returns a readable name for the current session.
// It is the exported form of generateSessionName.
func (a *Agent) GetSessionName() string {
	return a.generateSessionName()
}
