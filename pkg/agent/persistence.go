package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/agent_api"
)

const (
	scopedSessionsDirName = "scoped"
	legacySessionPrefix   = "session_"
	sessionRetentionLimit = 20
)

// Reset to default when running tests (helps with parallel test safety)
func init() {
	getStateDirFunc = defaultGetStateDir
}

// ConversationState represents the state of a conversation that can be persisted
type ConversationState struct {
	Messages                []api.Message    `json:"messages"`
	TurnCheckpoints         []TurnCheckpoint `json:"turn_checkpoints,omitempty"`
	TaskActions             []TaskAction     `json:"task_actions"`
	TotalCost               float64          `json:"total_cost"`
	TotalTokens             int              `json:"total_tokens"`
	PromptTokens            int              `json:"prompt_tokens"`
	CompletionTokens        int              `json:"completion_tokens"`
	EstimatedTokenResponses int              `json:"estimated_token_responses"`
	CachedTokens            int              `json:"cached_tokens"`
	CachedCostSavings       float64          `json:"cached_cost_savings"`
	LastUpdated             time.Time        `json:"last_updated"`
	SessionID               string           `json:"session_id"`
	Name                    string           `json:"name"`              // Human-readable session name
	WorkingDirectory        string           `json:"working_directory"` // Directory where session was created
}

// Variable to allow overriding GetStateDir for testing
var getStateDirFunc = defaultGetStateDir

// GetStateDir returns the directory for storing conversation state
func GetStateDir() (string, error) {
	return getStateDirFunc()
}

func normalizeSessionID(sessionID string) (string, error) {
	clean := strings.TrimSpace(sessionID)
	clean = strings.TrimPrefix(clean, legacySessionPrefix)
	if clean == "" {
		return "", errors.New("session ID cannot be empty")
	}
	if strings.Contains(clean, string(os.PathSeparator)) || strings.Contains(clean, "/") {
		return "", fmt.Errorf("session ID %q cannot contain path separators", sessionID)
	}
	return clean, nil
}

func normalizeWorkingDirectory(workingDir string) (string, error) {
	trimmed := strings.TrimSpace(workingDir)
	if trimmed == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("failed to resolve current working directory: %w", err)
		}
		trimmed = cwd
	}
	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return "", fmt.Errorf("failed to resolve absolute working directory %q: %w", trimmed, err)
	}
	return filepath.Clean(abs), nil
}

func workingDirectoryScopeHash(workingDir string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(workingDir))))
	return hex.EncodeToString(sum[:8])
}

func buildScopedSessionFilePath(stateDir, sessionID, workingDir string) (string, error) {
	cleanSessionID, err := normalizeSessionID(sessionID)
	if err != nil {
		return "", fmt.Errorf("failed to normalize session ID: %w", err)
	}
	cleanWorkingDir, err := normalizeWorkingDirectory(workingDir)
	if err != nil {
		return "", fmt.Errorf("failed to normalize working directory: %w", err)
	}
	scopeHash := workingDirectoryScopeHash(cleanWorkingDir)
	return filepath.Join(stateDir, scopedSessionsDirName, scopeHash, fmt.Sprintf("%s%s.json", legacySessionPrefix, cleanSessionID)), nil
}

func buildLegacySessionFilePath(stateDir, sessionID string) (string, error) {
	cleanSessionID, err := normalizeSessionID(sessionID)
	if err != nil {
		return "", fmt.Errorf("failed to normalize session ID: %w", err)
	}
	return filepath.Join(stateDir, fmt.Sprintf("%s%s.json", legacySessionPrefix, cleanSessionID)), nil
}

func listScopedSessionCandidates(stateDir, sessionID string) ([]string, error) {
	cleanSessionID, err := normalizeSessionID(sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize session ID: %w", err)
	}
	scopedRoot := filepath.Join(stateDir, scopedSessionsDirName)
	if _, err := os.Stat(scopedRoot); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to stat scoped sessions root: %w", err)
	}
	targetName := fmt.Sprintf("%s%s.json", legacySessionPrefix, cleanSessionID)
	var candidates []string
	walkErr := filepath.WalkDir(scopedRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("failed to walk path %s in scoped session directory: %w", path, err)
		}
		if d.IsDir() {
			return nil
		}
		if d.Name() == targetName {
			candidates = append(candidates, path)
		}
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("failed to scan scoped session directories: %w", walkErr)
	}
	return candidates, nil
}

func resolveSessionStateFile(stateDir, sessionID, workingDir string) (string, error) {
	scopedPath, scopedErr := buildScopedSessionFilePath(stateDir, sessionID, workingDir)
	if scopedErr == nil {
		if _, err := os.Stat(scopedPath); err == nil {
			return scopedPath, nil
		}
	}

	candidates, err := listScopedSessionCandidates(stateDir, sessionID)
	if err != nil {
		return "", fmt.Errorf("failed to list scoped session candidates: %w", err)
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	if len(candidates) > 1 {
		return "", fmt.Errorf("session ID %q is ambiguous across directories (%d matches); load with directory scope", sessionID, len(candidates))
	}

	legacyPath, err := buildLegacySessionFilePath(stateDir, sessionID)
	if err != nil {
		return "", fmt.Errorf("failed to build legacy session file path: %w", err)
	}
	if _, err := os.Stat(legacyPath); err == nil {
		return legacyPath, nil
	}
	return "", fmt.Errorf("session %q not found", sessionID)
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
	workingDir, _ := os.Getwd()
	return a.SaveStateScoped(sessionID, workingDir)
}

// SaveStateScoped saves conversation state under a directory-scoped session namespace.
func (a *Agent) SaveStateScoped(sessionID, workingDir string) error {
	stateDir, err := GetStateDir()
	if err != nil {
		return fmt.Errorf("failed to get state directory: %w", err)
	}
	cleanSessionID, err := normalizeSessionID(sessionID)
	if err != nil {
		return fmt.Errorf("invalid session ID: %w", err)
	}
	cleanWorkingDir, err := normalizeWorkingDirectory(workingDir)
	if err != nil {
		return fmt.Errorf("invalid working directory: %w", err)
	}
	stateFile, err := buildScopedSessionFilePath(stateDir, cleanSessionID, cleanWorkingDir)
	if err != nil {
		return fmt.Errorf("failed to build session file path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(stateFile), 0700); err != nil {
		return fmt.Errorf("failed to create scoped session directory: %w", err)
	}

	// Generate session name from first user message
	sessionName := a.generateSessionName()

	state := ConversationState{
		Messages:                a.messages,
		TurnCheckpoints:         a.copyTurnCheckpoints(),
		TaskActions:             a.taskActions,
		TotalCost:               a.totalCost,
		TotalTokens:             a.totalTokens,
		PromptTokens:            a.promptTokens,
		CompletionTokens:        a.completionTokens,
		EstimatedTokenResponses: a.estimatedTokenResponses,
		CachedTokens:            a.cachedTokens,
		CachedCostSavings:       a.cachedCostSavings,
		LastUpdated:             time.Now(),
		SessionID:               cleanSessionID,
		Name:                    sessionName,
		WorkingDirectory:        cleanWorkingDir,
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	return os.WriteFile(stateFile, data, 0600)
}

// LoadStateWithoutAgent loads a conversation state by session ID without an Agent instance
func LoadStateWithoutAgent(sessionID string) (*ConversationState, error) {
	workingDir, _ := os.Getwd()
	return LoadStateWithoutAgentScoped(sessionID, workingDir)
}

// LoadStateWithoutAgentScoped loads a state for a specific working directory scope.
func LoadStateWithoutAgentScoped(sessionID, workingDir string) (*ConversationState, error) {
	stateDir, err := GetStateDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get state directory: %w", err)
	}
	stateFile, err := resolveSessionStateFile(stateDir, sessionID, workingDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve session state file: %w", err)
	}

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

// LoadStateScoped loads a conversation state by session ID within a specific working directory scope.
func (a *Agent) LoadStateScoped(sessionID, workingDir string) (*ConversationState, error) {
	return LoadStateWithoutAgentScoped(sessionID, workingDir)
}

// ListSessionsWithTimestamps returns sessions for the current working directory scope.
func ListSessionsWithTimestamps() ([]SessionInfo, error) {
	workingDir, _ := os.Getwd()
	return ListSessionsWithTimestampsScoped(workingDir)
}

// ListAllSessionsWithTimestamps returns all available sessions across all scopes.
func ListAllSessionsWithTimestamps() ([]SessionInfo, error) {
	stateDir, err := GetStateDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get state directory: %w", err)
	}

	var sessions []SessionInfo
	walkErr := filepath.WalkDir(stateDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || filepath.Ext(d.Name()) != ".json" {
			return nil
		}
		session, ok := readSessionInfo(path, d)
		if ok {
			sessions = append(sessions, session)
		}
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("failed to scan session directory: %w", walkErr)
	}

	// Get current working directory for prioritization
	currentDir, _ := os.Getwd()

	// Sort sessions: current directory first, then by last updated (newest first)
	sort.Slice(sessions, func(i, j int) bool {
		// Always move current directory sessions to top
		iIsCurrent := sessions[i].WorkingDirectory == currentDir
		jIsCurrent := sessions[j].WorkingDirectory == currentDir
		if iIsCurrent != jIsCurrent {
			return iIsCurrent
		}

		// For same directory type, sort by last updated (newest first)
		return sessions[i].LastUpdated.After(sessions[j].LastUpdated)
	})

	return sessions, nil
}

// ListSessionsWithTimestampsScoped returns sessions only for the given working directory scope.
func ListSessionsWithTimestampsScoped(workingDir string) ([]SessionInfo, error) {
	stateDir, err := GetStateDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get state directory: %w", err)
	}
	cleanWorkingDir, err := normalizeWorkingDirectory(workingDir)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize working directory: %w", err)
	}

	sessionFiles, err := listSessionFilesForScope(stateDir, cleanWorkingDir)
	if err != nil {
		return nil, fmt.Errorf("failed to list session files for scope: %w", err)
	}

	sessions := make([]SessionInfo, 0, len(sessionFiles))
	for _, path := range sessionFiles {
		entry, err := os.Stat(path)
		if err != nil {
			continue
		}
		session, ok := readSessionInfo(path, fileInfoDirEntry{FileInfo: entry})
		if !ok {
			continue
		}
		if strings.TrimSpace(session.WorkingDirectory) != cleanWorkingDir {
			continue
		}
		sessions = append(sessions, session)
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastUpdated.After(sessions[j].LastUpdated)
	})
	return sessions, nil
}

// SessionInfo represents session information with timestamp
type SessionInfo struct {
	SessionID        string    `json:"session_id"`
	LastUpdated      time.Time `json:"last_updated"`
	Name             string    `json:"name"`              // Human-readable session name
	WorkingDirectory string    `json:"working_directory"` // Directory where session was created
	StoragePath      string    `json:"storage_path,omitempty"`
}

type fileInfoDirEntry struct {
	os.FileInfo
}

func (f fileInfoDirEntry) Type() os.FileMode          { return f.Mode().Type() }
func (f fileInfoDirEntry) Info() (os.FileInfo, error) { return f.FileInfo, nil }

func readSessionInfo(path string, d os.DirEntry) (SessionInfo, bool) {
	fileInfo, err := d.Info()
	if err != nil {
		return SessionInfo{}, false
	}

	lastUpdated := fileInfo.ModTime()
	name := ""
	workingDir := ""
	sessionID := strings.TrimSuffix(d.Name(), ".json")
	if strings.HasPrefix(sessionID, legacySessionPrefix) {
		sessionID = strings.TrimPrefix(sessionID, legacySessionPrefix)
	}
	if data, err := os.ReadFile(path); err == nil {
		var state ConversationState
		if err := json.Unmarshal(data, &state); err == nil {
			if !state.LastUpdated.IsZero() {
				lastUpdated = state.LastUpdated
			}
			name = state.Name
			if strings.TrimSpace(state.WorkingDirectory) != "" {
				normalizedWorkingDir, normErr := normalizeWorkingDirectory(state.WorkingDirectory)
				if normErr == nil {
					workingDir = normalizedWorkingDir
				}
			}
			if strings.TrimSpace(state.SessionID) != "" {
				sessionID = strings.TrimSpace(state.SessionID)
			}
		}
	}

	return SessionInfo{
		SessionID:        sessionID,
		LastUpdated:      lastUpdated,
		Name:             name,
		WorkingDirectory: workingDir,
		StoragePath:      path,
	}, true
}

func listSessionFilesForScope(stateDir, workingDir string) ([]string, error) {
	scopeDir := filepath.Join(stateDir, scopedSessionsDirName, workingDirectoryScopeHash(workingDir))
	files := make([]string, 0, 16)

	if entries, err := os.ReadDir(scopeDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
				continue
			}
			files = append(files, filepath.Join(scopeDir, entry.Name()))
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to read scoped session directory: %w", err)
	}

	// Include any legacy root sessions that explicitly recorded the same working directory.
	if entries, err := os.ReadDir(stateDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
				continue
			}
			files = append(files, filepath.Join(stateDir, entry.Name()))
		}
	} else {
		return nil, fmt.Errorf("failed to read session root directory: %w", err)
	}

	return files, nil
}

// GetSessionPreview returns the first 50 characters of the first user message
func GetSessionPreview(sessionID string) string {
	workingDir, _ := os.Getwd()
	return GetSessionPreviewScoped(sessionID, workingDir)
}

func GetSessionPreviewScoped(sessionID, workingDir string) string {
	stateDir, err := GetStateDir()
	if err != nil {
		return ""
	}
	stateFile, err := resolveSessionStateFile(stateDir, sessionID, workingDir)
	if err != nil {
		return ""
	}
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
	workingDir, _ := os.Getwd()
	return GetSessionNameScoped(sessionID, workingDir)
}

func GetSessionNameScoped(sessionID, workingDir string) string {
	stateDir, err := GetStateDir()
	if err != nil {
		return ""
	}
	stateFile, err := resolveSessionStateFile(stateDir, sessionID, workingDir)
	if err != nil {
		return ""
	}
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
	workingDir, _ := os.Getwd()
	return RenameSessionScoped(sessionID, newName, workingDir)
}

func RenameSessionScoped(sessionID, newName, workingDir string) error {
	stateDir, err := GetStateDir()
	if err != nil {
		return fmt.Errorf("failed to get state directory: %w", err)
	}
	stateFile, err := resolveSessionStateFile(stateDir, sessionID, workingDir)
	if err != nil {
		return fmt.Errorf("failed to resolve session file: %w", err)
	}

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
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}

	var sessionIDs []string
	for _, session := range sessions {
		sessionIDs = append(sessionIDs, session.SessionID)
	}

	return sessionIDs, nil
}

// DeleteSession removes a session state file
func DeleteSession(sessionID string) error {
	workingDir, _ := os.Getwd()
	return DeleteSessionScoped(sessionID, workingDir)
}

func DeleteSessionScoped(sessionID, workingDir string) error {
	stateDir, err := GetStateDir()
	if err != nil {
		return fmt.Errorf("failed to get state directory: %w", err)
	}
	stateFile, err := resolveSessionStateFile(stateDir, sessionID, workingDir)
	if err != nil {
		return fmt.Errorf("failed to resolve session file: %w", err)
	}
	if err := os.Remove(stateFile); err != nil {
		return fmt.Errorf("failed to delete session file %q: %w", stateFile, err)
	}
	return nil
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
	a.replaceTurnCheckpoints(state.TurnCheckpoints)
	a.taskActions = state.TaskActions
	a.totalCost = state.TotalCost
	a.totalTokens = state.TotalTokens
	a.promptTokens = state.PromptTokens
	a.completionTokens = state.CompletionTokens
	a.estimatedTokenResponses = state.EstimatedTokenResponses
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
	a.reasoningBuffer.Reset()

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

// cleanupMemorySessions removes old sessions for the current working directory scope.
func cleanupMemorySessions() error {
	workingDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to resolve current working directory for session cleanup: %w", err)
	}
	sessions, err := ListSessionsWithTimestampsScoped(workingDir)
	if err != nil {
		return fmt.Errorf("failed to list sessions: %w", err)
	}

	if len(sessions) <= sessionRetentionLimit {
		return nil // No cleanup needed
	}

	// Sort sessions by last updated (oldest first)
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastUpdated.Before(sessions[j].LastUpdated)
	})

	// Delete oldest sessions beyond the retention limit for this directory scope.
	for i := 0; i < len(sessions)-sessionRetentionLimit; i++ {
		if err := DeleteSessionScoped(sessions[i].SessionID, sessions[i].WorkingDirectory); err != nil {
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
