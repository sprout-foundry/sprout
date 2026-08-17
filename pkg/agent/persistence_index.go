package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/envutil"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/search"
)

const (
	scopedSessionsDirName = "scoped"
	legacySessionPrefix   = "session_"
	sessionRetentionLimit = 20
)

// Reset to default when running tests (helps with parallel test safety)
func init() {
	getStateDirFunc = defaultGetStateDir
	// Initialize the search index updater for debounced incremental updates.
	sd, err := defaultGetStateDir()
	if err == nil {
		search.InitGlobalUpdater(filepath.Join(sd, "search-index.json"), sd)
	}
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
	ContinuationNudges      int              `json:"continuation_nudges,omitempty"` // seed transient "continue" nudges observed (invisible in messages)
	CachedTokens            int              `json:"cached_tokens"`
	CacheWriteTokens        int              `json:"cache_write_tokens,omitempty"`
	CachedCostSavings       float64          `json:"cached_cost_savings"`
	ImageTokens             int              `json:"image_tokens,omitempty"`
	LastUpdated             time.Time        `json:"last_updated"`
	SessionID               string           `json:"session_id"`
	Name                    string           `json:"name"`              // Human-readable session name
	WorkingDirectory        string           `json:"working_directory"` // Directory where session was created

	// ConfigOverrides stores session-scoped configuration overrides.
	// Applied on top of global and workspace config when the session is restored.
	// Only non-empty values are considered overrides.
	ConfigOverrides map[string]interface{} `json:"config_overrides,omitempty"`

	// SessionIntentEmbedding stores the embedding of the first user prompt in a session.
	// Used for drift detection to track conversation intent over time.
	SessionIntentEmbedding []float32 `json:"session_intent_embedding,omitempty"`

	// LastProviderError captures details about the last API error from the LLM provider.
	// Persisted in the session file so errors can be diagnosed after the fact.
	LastProviderError *ProviderErrorInfo `json:"last_provider_error,omitempty"`
}

// ProviderErrorInfo captures details about the last API error from the LLM provider.
// This is persisted in the session file so errors can be diagnosed after the fact.
type ProviderErrorInfo struct {
	Timestamp  string `json:"timestamp"`             // ISO 8601 when the error occurred
	Provider   string `json:"provider"`              // e.g. "zai", "openrouter"
	Model      string `json:"model"`                 // e.g. "glm-5.1"
	StatusCode int    `json:"status_code,omitempty"` // HTTP status code (400, 429, 500, etc.)
	ErrorType  string `json:"error_type,omitempty"`  // e.g. "api_error_400", "streaming_response"
	Message    string `json:"message"`               // The error message from the provider
	Retries    int    `json:"retries,omitempty"`     // Number of retries attempted
}

// Variable to allow overriding GetStateDir for testing
var getStateDirFunc = defaultGetStateDir

// GetStateDir returns the directory for storing conversation state
func GetStateDir() (string, error) {
	return getStateDirFunc()
}

// defaultGetStateDir is the actual implementation of GetStateDir
func defaultGetStateDir() (string, error) {
	stateDir, err := envutil.StateDir()
	if err != nil {
		return "", agenterrors.NewAgent("persistence", "failed to resolve state directory", err)
	}
	sessionsDir := filepath.Join(stateDir, "sessions")
	if err := os.MkdirAll(sessionsDir, 0700); err != nil {
		return "", agenterrors.NewAgent("persistence", "failed to create sessions directory", err)
	}

	return sessionsDir, nil
}

// GenerateSessionSummary creates a summary of previous actions for continuity
func (a *Agent) GenerateSessionSummary() string {
	taskActions := a.GetTaskActions()
	if len(taskActions) == 0 {
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

	for _, action := range taskActions {
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
	summary.WriteString(fmt.Sprintf("• Total cost: $%.6f\n", a.state.GetTotalCost()))
	summary.WriteString(fmt.Sprintf("• Total tokens: %s\n", a.formatTokenCount(a.state.GetTotalTokens())))

	// Add recent notable actions
	if len(taskActions) > 0 {
		summary.WriteString("\nRecent actions:\n")
		recentCount := min(5, len(taskActions))
		for i := len(taskActions) - recentCount; i < len(taskActions); i++ {
			action := taskActions[i]
			summary.WriteString(fmt.Sprintf("• %s: %s\n", action.Type, action.Description))
		}
	}

	summary.WriteString("=====================================\n")

	return summary.String()
}

// ExportStateToJSON converts a ConversationState to JSON bytes
func ExportStateToJSON(state *ConversationState) ([]byte, error) {
	return json.MarshalIndent(state, "", "  ")
}

// ImportStateFromJSONFile loads a ConversationState from a JSON file
func ImportStateFromJSONFile(filename string) (*ConversationState, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, agenterrors.NewAgent("persistence", "failed to read import file", err)
	}

	var state ConversationState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, agenterrors.NewAgent("persistence", "failed to unmarshal state from file", err)
	}

	return &state, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// === Test Helpers ===

// SetGetStateDirFunc sets the getStateDirFunc for testing purposes.
// Returns the previous function so it can be restored after the test.
func SetGetStateDirFunc(fn func() (string, error)) func() (string, error) {
	old := getStateDirFunc
	getStateDirFunc = fn
	return old
}

// SetGetStateDirForTest is a convenience helper that sets getStateDirFunc
// to return a fixed directory for testing.
func SetGetStateDirForTest(dir string) func() (string, error) {
	return SetGetStateDirFunc(func() (string, error) {
		return dir, nil
	})
}

// SetGetStateDirForTestError is a convenience helper that sets getStateDirFunc
// to return an error for testing error handling.
func SetGetStateDirForTestError(msg string) func() (string, error) {
	err := agenterrors.NewAgent("persistence", msg, nil)
	return SetGetStateDirFunc(func() (string, error) {
		return "", err
	})
}
