package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/search"
)

func normalizeSessionID(sessionID string) (string, error) {
	clean := strings.TrimSpace(sessionID)
	clean = strings.TrimPrefix(clean, legacySessionPrefix)
	if clean == "" {
		return "", agenterrors.NewInvalidInputError("session ID cannot be empty", nil)
	}
	if strings.Contains(clean, string(os.PathSeparator)) || strings.Contains(clean, "/") {
		return "", agenterrors.NewValidation(fmt.Sprintf("session ID %q cannot contain path separators", sessionID), nil)
	}
	return clean, nil
}

func normalizeWorkingDirectory(workingDir string) (string, error) {
	trimmed := strings.TrimSpace(workingDir)
	if trimmed == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", agenterrors.Wrap(err, "failed to resolve current working directory")
		}
		trimmed = cwd
	}
	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return "", agenterrors.Wrapf(err, "failed to resolve absolute working directory %q", trimmed)
	}
	// Resolve symlinks for consistent path comparison. On macOS,
	// /var → /private/var and os.Getwd() returns the resolved path,
	// while t.TempDir() returns the unresolved path.
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return filepath.Clean(resolved), nil
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
		return "", agenterrors.Wrap(err, "failed to normalize session ID")
	}
	cleanWorkingDir, err := normalizeWorkingDirectory(workingDir)
	if err != nil {
		return "", agenterrors.Wrap(err, "failed to normalize working directory")
	}
	scopeHash := workingDirectoryScopeHash(cleanWorkingDir)
	return filepath.Join(stateDir, scopedSessionsDirName, scopeHash, fmt.Sprintf("%s%s.json", legacySessionPrefix, cleanSessionID)), nil
}

func buildLegacySessionFilePath(stateDir, sessionID string) (string, error) {
	cleanSessionID, err := normalizeSessionID(sessionID)
	if err != nil {
		return "", agenterrors.Wrap(err, "failed to normalize session ID")
	}
	return filepath.Join(stateDir, fmt.Sprintf("%s%s.json", legacySessionPrefix, cleanSessionID)), nil
}

func listScopedSessionCandidates(stateDir, sessionID string) ([]string, error) {
	cleanSessionID, err := normalizeSessionID(sessionID)
	if err != nil {
		return nil, agenterrors.Wrap(err, "failed to normalize session ID")
	}
	scopedRoot := filepath.Join(stateDir, scopedSessionsDirName)
	if _, err := os.Stat(scopedRoot); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, agenterrors.Wrap(err, "failed to stat scoped sessions root")
	}
	targetName := fmt.Sprintf("%s%s.json", legacySessionPrefix, cleanSessionID)
	var candidates []string
	walkErr := filepath.WalkDir(scopedRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return agenterrors.Wrapf(err, "failed to walk path %s in scoped session directory", path)
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
		return nil, agenterrors.Wrap(walkErr, "failed to scan scoped session directories")
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
		return "", agenterrors.Wrap(err, "failed to list scoped session candidates")
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	if len(candidates) > 1 {
		return "", agenterrors.NewValidation(fmt.Sprintf("session ID %q is ambiguous across directories (%d matches); load with directory scope", sessionID, len(candidates)), nil)
	}

	legacyPath, err := buildLegacySessionFilePath(stateDir, sessionID)
	if err != nil {
		return "", agenterrors.Wrap(err, "failed to build legacy session file path")
	}
	if _, err := os.Stat(legacyPath); err == nil {
		return legacyPath, nil
	}
	return "", agenterrors.NewNotFound(fmt.Sprintf("session %q", sessionID))
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
		return agenterrors.NewAgent("persistence", "failed to get state directory", err)
	}
	cleanSessionID, err := normalizeSessionID(sessionID)
	if err != nil {
		return agenterrors.Wrap(err, "invalid session ID")
	}
	cleanWorkingDir, err := normalizeWorkingDirectory(workingDir)
	if err != nil {
		return agenterrors.Wrap(err, "invalid working directory")
	}
	stateFile, err := buildScopedSessionFilePath(stateDir, cleanSessionID, cleanWorkingDir)
	if err != nil {
		return agenterrors.NewAgent("persistence", "failed to build session file path", err)
	}
	if err := os.MkdirAll(filepath.Dir(stateFile), 0700); err != nil {
		return agenterrors.NewAgent("persistence", "failed to create scoped session directory", err)
	}

	// Generate session name from first user message
	sessionName := a.generateSessionName()

	state := ConversationState{
		Messages:                a.state.GetMessages(),
		TurnCheckpoints:         a.copyTurnCheckpoints(),
		TaskActions:             a.GetTaskActions(),
		TotalCost:               a.state.GetTotalCost(),
		TotalTokens:             a.state.GetTotalTokens(),
		PromptTokens:            a.state.GetPromptTokens(),
		CompletionTokens:        a.state.GetCompletionTokens(),
		EstimatedTokenResponses: a.state.GetEstimatedTokenResponses(),
		CachedTokens:            a.state.GetCachedTokens(),
		CacheWriteTokens:        a.state.GetCacheWriteTokens(),
		CachedCostSavings:       a.state.GetCachedCostSavings(),
		LastUpdated:             time.Now(),
		SessionID:               cleanSessionID,
		Name:                    sessionName,
		WorkingDirectory:        cleanWorkingDir,
		ConfigOverrides:         a.state.GetConfigOverrides(),
		SessionIntentEmbedding:  a.state.GetSessionIntentEmbedding(),
		LastProviderError:       a.state.GetLastProviderError(),
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return agenterrors.NewAgent("persistence", "failed to marshal state", err)
	}

	if err := backupFileWithExt(stateFile, ".bak"); err != nil {
		a.Logger().Debug("[WARN] Failed to back up session state before save: %v\n", err)
	}
	if err := writeFileAtomic(stateFile, data, 0o600); err != nil {
		return agenterrors.NewAgent("persistence", "failed to write session state atomically", err)
	}
	search.MarkSessionDirty(cleanSessionID)

	// Fire-and-forget training data push. This never blocks or fails
	// the session save — the goroutine logs errors to stderr.
	// The push function (if wired) applies PII redaction before
	// sending data over the network.
	a.pushTrainingSession(state)
	return nil
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
		return nil, agenterrors.NewAgent("persistence", "failed to get state directory", err)
	}
	stateFile, err := resolveSessionStateFile(stateDir, sessionID, workingDir)
	if err != nil {
		return nil, agenterrors.NewAgent("persistence", "failed to resolve session state file", err)
	}

	data, err := os.ReadFile(stateFile)
	if err != nil {
		return nil, agenterrors.NewAgent("persistence", "failed to read state file", err)
	}

	var state ConversationState
	if err := json.Unmarshal(data, &state); err != nil {
		if bak, bakErr := loadBackupState(stateFile); bakErr == nil {
			return bak, nil
		}
		return nil, agenterrors.NewAgent("persistence", "failed to unmarshal state", err)
	}

	return &state, nil
}

func loadBackupState(stateFile string) (*ConversationState, error) {
	data, err := os.ReadFile(stateFile + ".bak")
	if err != nil {
		return nil, agenterrors.Wrap(err, "failed to read state backup")
	}
	var state ConversationState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, agenterrors.Wrap(err, "failed to unmarshal state backup")
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

// ApplyState applies a loaded state to the current agent
func (a *Agent) ApplyState(state *ConversationState) {
	// Apply saved state
	a.state.SetMessages(state.Messages)
	a.ReplaceTurnCheckpoints(state.TurnCheckpoints)
	a.replaceTaskActions(state.TaskActions)
	a.state.SetTotalCost(state.TotalCost)
	a.state.SetTotalTokens(state.TotalTokens)
	a.state.SetPromptTokens(state.PromptTokens)
	a.state.SetCompletionTokens(state.CompletionTokens)
	a.state.SetEstimatedTokenResponses(state.EstimatedTokenResponses)
	a.state.SetCachedTokens(state.CachedTokens)
	a.state.SetCacheWriteTokens(state.CacheWriteTokens)
	a.state.SetCachedCostSavings(state.CachedCostSavings)
	a.state.SetImageTokens(state.ImageTokens)

	// CRITICAL: Reset session state to prevent hanging issues after session restore
	a.state.SetCurrentIteration(0)
	a.state.SetContextWarningIssued(false)

	// Restore session intent embedding for drift detection
	a.state.SetSessionIntentEmbedding(state.SessionIntentEmbedding)

	// Restore last provider error info
	a.state.SetLastProviderError(state.LastProviderError)

	// Reset circuit breaker state to prevent false positives
	if a.state.GetCircuitBreaker() != nil {
		a.state.GetCircuitBreaker().mu.Lock()
		// Clear entries instead of replacing map to avoid memory churn and reduce lock hold time
		for key := range a.state.GetCircuitBreaker().Actions {
			delete(a.state.GetCircuitBreaker().Actions, key)
		}
		a.state.GetCircuitBreaker().mu.Unlock()
	}

	// Clear streaming buffer to prevent old content from interfering
	a.output.GetStreamingBuffer().Reset()
	a.output.GetReasoningBuffer().Reset()

	// Reset shell command history to prevent stale cache issues
	a.ClearShellCommandHistory()
}

// GetLastMessages returns the last N messages for preview
func (a *Agent) GetLastMessages(n int) []api.Message {
	messages := a.state.GetMessages()
	if len(messages) == 0 {
		return []api.Message{}
	}

	start := len(messages) - n
	if start < 0 {
		start = 0
	}

	return messages[start:]
}
