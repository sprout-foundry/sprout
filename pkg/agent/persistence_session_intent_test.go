package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSessionIntentEmbedding verifies that SessionIntentEmbedding is correctly
// persisted and restored across save/load cycles.
func TestSessionIntentEmbedding(t *testing.T) {
	defer NewTestStateDir(t)()
	wd := t.TempDir()

	agent := &Agent{
		state: NewAgentStateManager(false),
	}
	agent.state.SetSessionID("test-embedding-session")
	agent.state.AddMessage(api.Message{Role: "user", Content: "Test user message"})

	// Set a session intent embedding
	expectedEmbedding := []float32{0.1, 0.2, 0.3, 0.4, 0.5}
	agent.state.SetSessionIntentEmbedding(expectedEmbedding)

	// Save the state
	err := agent.SaveStateScoped("test-embedding-session", wd)
	require.NoError(t, err)

	// Load the state
	loadedState, err := LoadStateWithoutAgentScoped("test-embedding-session", wd)
	require.NoError(t, err)

	// Verify the embedding was saved correctly
	assert.NotNil(t, loadedState.SessionIntentEmbedding, "SessionIntentEmbedding should not be nil after save/load")
	assert.Equal(t, expectedEmbedding, loadedState.SessionIntentEmbedding, "SessionIntentEmbedding should match the original value")

	// Verify the embedding can be loaded by ConversationState directly (without agent)
	var state ConversationState
	stateDir, _ := GetStateDir()
	stateFile, _ := resolveSessionStateFile(stateDir, "test-embedding-session", wd)
	data, _ := os.ReadFile(stateFile)
	err = json.Unmarshal(data, &state)
	require.NoError(t, err)
	assert.Equal(t, expectedEmbedding, state.SessionIntentEmbedding, "ConversationState should contain the embedding")

	// Verify the JSON file contains the session_intent_embedding field
	assert.Contains(t, string(data), "session_intent_embedding", "JSON should contain session_intent_embedding field when not nil")
}

// TestSessionIntentEmbeddingNil verifies that nil embeddings are handled correctly.
func TestSessionIntentEmbeddingNil(t *testing.T) {
	defer NewTestStateDir(t)()
	wd := t.TempDir()

	agent := &Agent{
		state: NewAgentStateManager(false),
	}
	agent.state.SetSessionID("test-nil-embedding-session")

	// Verify the getter returns nil when not set
	assert.Nil(t, agent.state.GetSessionIntentEmbedding(), "SessionIntentEmbedding should be nil when not set")

	// Save state with nil embedding
	err := agent.SaveStateScoped("test-nil-embedding-session", wd)
	require.NoError(t, err)

	// Load the state
	loadedState, err := LoadStateWithoutAgentScoped("test-nil-embedding-session", wd)
	require.NoError(t, err)

	// Verify the embedding is nil after loading
	assert.Nil(t, loadedState.SessionIntentEmbedding, "SessionIntentEmbedding should be nil after load when not originally set")

	// Verify the JSON file doesn't contain a session_intent_embedding field when nil.
	// Normalize to match the symlink-resolved path used by SaveStateScoped.
	stateDir, _ := GetStateDir()
	normalizedWd, evalErr := normalizeWorkingDirectory(wd)
	if evalErr != nil {
		t.Fatalf("normalize working dir: %v", evalErr)
	}
	stateFile := filepath.Join(stateDir, "scoped", workingDirectoryScopeHash(normalizedWd), "session_test-nil-embedding-session.json")
	data, err := os.ReadFile(stateFile)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "session_intent_embedding", "JSON should not contain session_intent_embedding field when nil")
}

// TestSessionIntentEmbeddingEmptySlice verifies that empty slices are handled correctly.
func TestSessionIntentEmbeddingEmptySlice(t *testing.T) {
	agent := &Agent{
		state: NewAgentStateManager(false),
	}
	agent.state.SetSessionID("test-empty-embedding-session")

	// Set an empty embedding (should be treated as nil)
	emptyEmbedding := []float32{}
	agent.state.SetSessionIntentEmbedding(emptyEmbedding)

	// Verify the getter returns nil for empty slice
	assert.Nil(t, agent.state.GetSessionIntentEmbedding(), "SessionIntentEmbedding should be nil when set to empty slice")
}

// TestSetSessionIntentEmbeddingIfNil verifies the atomic check-and-set behavior.
func TestSetSessionIntentEmbeddingIfNil(t *testing.T) {
	mgr := NewAgentStateManager(false)

	// Initially nil — should succeed
	emb1 := []float32{0.1, 0.2, 0.3}
	ok := mgr.SetSessionIntentEmbeddingIfNil(emb1)
	assert.True(t, ok, "Should return true when setting from nil")
	assert.Equal(t, emb1, mgr.GetSessionIntentEmbedding(), "Embedding should be set")

	// Already set — should not overwrite
	emb2 := []float32{0.9, 0.8, 0.7}
	ok = mgr.SetSessionIntentEmbeddingIfNil(emb2)
	assert.False(t, ok, "Should return false when already set")
	assert.Equal(t, emb1, mgr.GetSessionIntentEmbedding(), "Embedding should remain unchanged")

	// Nil input — should return false
	mgr2 := NewAgentStateManager(false)
	ok = mgr2.SetSessionIntentEmbeddingIfNil(nil)
	assert.False(t, ok, "Should return false for nil input")
	assert.Nil(t, mgr2.GetSessionIntentEmbedding())

	// Empty slice input — should return false
	ok = mgr2.SetSessionIntentEmbeddingIfNil([]float32{})
	assert.False(t, ok, "Should return false for empty slice input")
	assert.Nil(t, mgr2.GetSessionIntentEmbedding())
}

// TestSessionIntentEmbeddingLegacyJSON verifies backward compatibility with
// session files that do not contain session_intent_embedding.
func TestSessionIntentEmbeddingLegacyJSON(t *testing.T) {
	defer NewTestStateDir(t)()
	wd := t.TempDir()

	normalizedWd, err := normalizeWorkingDirectory(wd)
	require.NoError(t, err)

	stateDir, _ := GetStateDir()

	// Write a legacy JSON file without session_intent_embedding
	legacyJSON := `{
		"messages": [{"role": "user", "content": "hello"}],
		"session_id": "legacy-session",
		"working_directory": "` + filepath.ToSlash(normalizedWd) + `"
	}`
	stateFile := filepath.Join(stateDir, "scoped", workingDirectoryScopeHash(normalizedWd), "session_legacy-session.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(stateFile), 0700))
	require.NoError(t, os.WriteFile(stateFile, []byte(legacyJSON), 0600))

	// Load should succeed with nil embedding
	loadedState, err := LoadStateWithoutAgentScoped("legacy-session", wd)
	require.NoError(t, err)
	assert.Nil(t, loadedState.SessionIntentEmbedding, "Legacy sessions should have nil SessionIntentEmbedding")
}
