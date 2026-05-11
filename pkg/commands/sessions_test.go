package commands

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// Test helper to create an isolated test agent
func newIsolatedTestAgent(t *testing.T) *agent.Agent {
	t.Helper()

	configDir := t.TempDir() + "/.sprout"

	// Set both env vars for isolation (NewAgentWithModel reads SPROUT_CONFIG/LEDIT_CONFIG
	// to determine the config directory; the env vars ensure the agent uses our temp dir)
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)

	testAgent, err := agent.NewAgentWithModel("test:test")
	if err != nil {
		t.Fatalf("NewAgentWithModel failed: %v", err)
	}

	return testAgent
}

// Test helper to compute working directory scope hash
func hashWorkingDir(wd string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(wd))))
	return hex.EncodeToString(sum[:8])
}

// Test helper to create a session file in the state directory
func createSessionFile(t *testing.T, stateDir, sessionID, workingDir string, name string) {
	t.Helper()

	scopeHash := hashWorkingDir(workingDir)
	scopeDir := filepath.Join(stateDir, "scoped", scopeHash)
	if err := os.MkdirAll(scopeDir, 0700); err != nil {
		t.Fatalf("failed to create scope dir: %v", err)
	}

	// Create a minimal ConversationState
	state := &agent.ConversationState{
		SessionID:        sessionID,
		Name:             name,
		WorkingDirectory: workingDir,
		Messages:         []api.Message{},
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal state: %v", err)
	}

	sessionFile := filepath.Join(scopeDir, fmt.Sprintf("session_%s.json", sessionID))
	if err := os.WriteFile(sessionFile, data, 0600); err != nil {
		t.Fatalf("failed to write session file: %v", err)
	}
}

// Test helper to capture stdout
func captureOutput(f func()) string {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	f()

	os.Stdout = oldStdout
	w.Close() // Close write end first to flush

	var buf bytes.Buffer
	buf.ReadFrom(r)
	r.Close() // Close read end to avoid fd leak
	return buf.String()
}

// TestSessionsCommand_Name tests the Name method
func TestSessionsCommand_Name(t *testing.T) {
	cmd := &SessionsCommand{}
	assert.Equal(t, "sessions", cmd.Name())
}

// TestSessionsCommand_Description tests the Description method
func TestSessionsCommand_Description(t *testing.T) {
	cmd := &SessionsCommand{}
	desc := cmd.Description()
	assert.Contains(t, desc, "previous conversation")
	assert.Contains(t, desc, "session")
}

// TestSessionsCommand_Execute_NoSessions tests execution when no sessions exist
func TestSessionsCommand_Execute_NoSessions(t *testing.T) {
	// Create a temp state directory
	tempStateDir := t.TempDir()

	// Override getStateDirFunc to use temp directory
	originalGetStateDirFunc := agent.SetGetStateDirForTest(tempStateDir)
	defer agent.SetGetStateDirFunc(originalGetStateDirFunc)

	// Create test agent
	testAgent := newIsolatedTestAgent(t)
	defer testAgent.Shutdown()

	// Create command and execute
	cmd := &SessionsCommand{}

	output := captureOutput(func() {
		err := cmd.Execute([]string{}, testAgent)
		assert.NoError(t, err)
	})

	// Verify the "No saved sessions found." message
	assert.Contains(t, output, "No saved sessions found")
}

// TestSessionsCommand_Execute_WithArgs_DirectLoad tests direct session loading with args
func TestSessionsCommand_Execute_WithArgs_DirectLoad(t *testing.T) {
	// Create a temp state directory
	tempStateDir := t.TempDir()

	// Override getStateDirFunc to use temp directory
	originalGetStateDirFunc := agent.SetGetStateDirForTest(tempStateDir)
	defer agent.SetGetStateDirFunc(originalGetStateDirFunc)

	// Get the actual working directory
	workingDir, err := os.Getwd()
	require.NoError(t, err)
	sessionID := "test-session-001"

	// Create a session file with messages
	createSessionFile(t, tempStateDir, sessionID, workingDir, "Test Session")

	// Load the session file and add messages
	scopeHash := hashWorkingDir(workingDir)
	sessionFile := filepath.Join(tempStateDir, "scoped", scopeHash, fmt.Sprintf("session_%s.json", sessionID))
	data, err := os.ReadFile(sessionFile)
	require.NoError(t, err)

	var state agent.ConversationState
	err = json.Unmarshal(data, &state)
	require.NoError(t, err)

	// Add some test messages
	state.Messages = []api.Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there!"},
	}

	updatedData, err := json.MarshalIndent(state, "", "  ")
	require.NoError(t, err)
	err = os.WriteFile(sessionFile, updatedData, 0600)
	require.NoError(t, err)

	// Create test agent
	testAgent := newIsolatedTestAgent(t)
	defer testAgent.Shutdown()

	// Execute command with session number arg
	cmd := &SessionsCommand{}

	output := captureOutput(func() {
		err := cmd.Execute([]string{"1"}, testAgent)
		assert.NoError(t, err)
	})

	// Verify success message
	assert.Contains(t, output, "[ok] Conversation session loaded")
	assert.Contains(t, output, sessionID)

	// Verify the messages were loaded
	messages := testAgent.GetMessages()
	assert.Len(t, messages, 2)
	assert.Equal(t, "user", messages[0].Role)
	assert.Equal(t, "Hello", messages[0].Content)
	assert.Equal(t, "assistant", messages[1].Role)
	assert.Equal(t, "Hi there!", messages[1].Content)
}

// TestSessionsCommand_Execute_WithArgs_InvalidNumber tests invalid session number
func TestSessionsCommand_Execute_WithArgs_InvalidNumber(t *testing.T) {
	// Create a temp state directory
	tempStateDir := t.TempDir()

	// Override getStateDirFunc to use temp directory
	originalGetStateDirFunc := agent.SetGetStateDirForTest(tempStateDir)
	defer agent.SetGetStateDirFunc(originalGetStateDirFunc)

	// Get the actual working directory
	workingDir, err := os.Getwd()
	require.NoError(t, err)

	// Create a session
	createSessionFile(t, tempStateDir, "session-001", workingDir, "Session 1")

	// Create test agent
	testAgent := newIsolatedTestAgent(t)
	defer testAgent.Shutdown()

	cmd := &SessionsCommand{}

	tests := []struct {
		name      string
		arg       string
		wantError string
	}{
		{"non_numeric", "abc", "invalid session number"},
		{"out_of_range_high", "999", "invalid session number"},
		{"out_of_range_low", "0", "invalid session number"},
		{"negative", "-1", "invalid session number"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := cmd.Execute([]string{tt.arg}, testAgent)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantError)
		})
	}
}

// TestSessionsCommand_Execute_ListSessionsError tests error when listing sessions fails
func TestSessionsCommand_Execute_ListSessionsError(t *testing.T) {
	// Override getStateDirFunc to return an error
	originalGetStateDirFunc := agent.SetGetStateDirForTestError("failed to get state dir")
	defer agent.SetGetStateDirFunc(originalGetStateDirFunc)

	// Create test agent
	testAgent := newIsolatedTestAgent(t)
	defer testAgent.Shutdown()

	cmd := &SessionsCommand{}

	err := cmd.Execute([]string{}, testAgent)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list sessions")
}

// TestSessionsCommand_Execute_LoadSessionError tests error when loading session fails
func TestSessionsCommand_Execute_LoadSessionError(t *testing.T) {
	// Create a temp state directory
	tempStateDir := t.TempDir()

	// Override getStateDirFunc to use temp directory
	originalGetStateDirFunc := agent.SetGetStateDirForTest(tempStateDir)
	defer agent.SetGetStateDirFunc(originalGetStateDirFunc)

	// Get the actual working directory
	workingDir, err := os.Getwd()
	require.NoError(t, err)

	// Create a valid session so it shows up in the list
	sessionID := "test-session"
	createSessionFile(t, tempStateDir, sessionID, workingDir, "Test Session")

	// Create test agent
	testAgent := newIsolatedTestAgent(t)
	defer testAgent.Shutdown()

	cmd := &SessionsCommand{}

	// Try to load session 2 which doesn't exist
	err = cmd.Execute([]string{"2"}, testAgent)
	// The error should occur when trying to load an out-of-range session
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid session number")
}

// TestSessionsCommand_selectSessionWithDropdown_AgentConsole tests agent console mode
func TestSessionsCommand_selectSessionWithDropdown_AgentConsole(t *testing.T) {
	// Create a temp state directory
	tempStateDir := t.TempDir()

	// Override getStateDirFunc to use temp directory
	originalGetStateDirFunc := agent.SetGetStateDirForTest(tempStateDir)
	defer agent.SetGetStateDirFunc(originalGetStateDirFunc)

	// Get the actual working directory
	workingDir, err := os.Getwd()
	require.NoError(t, err)

	// Create test sessions
	createSessionFile(t, tempStateDir, "session-001", workingDir, "Session 1")
	createSessionFile(t, tempStateDir, "session-002", workingDir, "Session 2")

	// Create test agent
	testAgent := newIsolatedTestAgent(t)
	defer testAgent.Shutdown()

	// Set AGENT_CONSOLE env var BEFORE calling Execute
	// GetEnvSimple("AGENT_CONSOLE") checks SPROUT_AGENT_CONSOLE and LEDIT_AGENT_CONSOLE
	t.Setenv("SPROUT_AGENT_CONSOLE", "1")

	cmd := &SessionsCommand{}

	output := captureOutput(func() {
		err := cmd.Execute([]string{}, testAgent)
		assert.NoError(t, err)
	})

	// Verify output contains expected sections
	assert.Contains(t, output, "Available Sessions:")
	assert.Contains(t, output, "session-001")
	assert.Contains(t, output, "session-002")
	assert.Contains(t, output, "[i] To load a session, use: /sessions <session_number>")
	assert.Contains(t, output, "Example: /sessions 1")
}

// TestSessionsCommand_selectSessionWithDropdown_NonTerminal tests non-terminal mode
func TestSessionsCommand_selectSessionWithDropdown_NonTerminal(t *testing.T) {
	// Create a temp state directory
	tempStateDir := t.TempDir()

	// Override getStateDirFunc to use temp directory
	originalGetStateDirFunc := agent.SetGetStateDirForTest(tempStateDir)
	defer agent.SetGetStateDirFunc(originalGetStateDirFunc)

	// Ensure AGENT_CONSOLE is not set
	t.Setenv("SPROUT_AGENT_CONSOLE", "")
	t.Setenv("LEDIT_AGENT_CONSOLE", "")

	// Get the actual working directory
	workingDir, err := os.Getwd()
	require.NoError(t, err)

	// Create test sessions
	createSessionFile(t, tempStateDir, "session-001", workingDir, "Session 1")

	// Create test agent
	testAgent := newIsolatedTestAgent(t)
	defer testAgent.Shutdown()

	sessions := []agent.SessionInfo{
		{
			SessionID:   "session-001",
			Name:        "Session 1",
			LastUpdated: agentTime("2024-01-01T10:00:00Z"),
		},
	}

	cmd := &SessionsCommand{}

	output := captureOutput(func() {
		err := cmd.selectSessionWithDropdown(sessions, testAgent)
		assert.NoError(t, err)
	})

	// Should show message about using direct selection
	// Note: In a real test environment without a terminal, this path is taken
	assert.Contains(t, output, "Interactive session selection requires a terminal")
}

// TestSessionsCommand_selectSessionWithDropdown_Interactive tests interactive selection
func TestSessionsCommand_selectSessionWithDropdown_Interactive(t *testing.T) {
	// This test is skipped because it requires actual terminal input
	// In production, this would need stdin pipe setup
	t.Skip("Interactive selection test requires terminal input simulation")
}

// TestDisplayConversationPreview tests the conversation preview display
func TestDisplayConversationPreview(t *testing.T) {
	testAgent := newIsolatedTestAgent(t)
	defer testAgent.Shutdown()

	// Add messages to the agent state
	testAgent.AddMessage(api.Message{Role: "user", Content: "First message"})
	testAgent.AddMessage(api.Message{Role: "assistant", Content: "First response"})
	testAgent.AddMessage(api.Message{Role: "user", Content: "Second message"})
	testAgent.AddMessage(api.Message{Role: "assistant", Content: "Second response"})

	output := captureOutput(func() {
		displayConversationPreview(testAgent)
	})

	// Verify preview output
	assert.Contains(t, output, "Recent conversation preview")
	assert.Contains(t, output, "[you] You: First message")
	assert.Contains(t, output, "[bot] Assistant: First response")
	assert.Contains(t, output, "[you] You: Second message")
	assert.Contains(t, output, "[bot] Assistant: Second response")
}

// TestDisplayConversationPreview_Empty tests preview with no messages
func TestDisplayConversationPreview_Empty(t *testing.T) {
	testAgent := newIsolatedTestAgent(t)
	defer testAgent.Shutdown()

	// No messages added — displayConversationPreview prints nothing when GetLastMessages returns empty
	output := captureOutput(func() {
		displayConversationPreview(testAgent)
	})

	assert.Empty(t, output, "displayConversationPreview should produce no output when there are no messages")
}

// TestSessionsCommand_MultipleSessions tests listing and selecting from multiple sessions
func TestSessionsCommand_MultipleSessions(t *testing.T) {
	// Create a temp state directory
	tempStateDir := t.TempDir()

	// Override getStateDirFunc to use temp directory
	originalGetStateDirFunc := agent.SetGetStateDirForTest(tempStateDir)
	defer agent.SetGetStateDirFunc(originalGetStateDirFunc)

	// Get the actual working directory
	workingDir, err := os.Getwd()
	require.NoError(t, err)

	// Create multiple sessions
	createSessionFile(t, tempStateDir, "session-001", workingDir, "First Session")
	createSessionFile(t, tempStateDir, "session-002", workingDir, "Second Session")
	createSessionFile(t, tempStateDir, "session-003", workingDir, "Third Session")

	// Create test agent
	testAgent := newIsolatedTestAgent(t)
	defer testAgent.Shutdown()

	// Set AGENT_CONSOLE to see the list without interactive prompt
	// IMPORTANT: Set env var before Execute is called
	// GetEnvSimple("AGENT_CONSOLE") checks SPROUT_AGENT_CONSOLE and LEDIT_AGENT_CONSOLE
	t.Setenv("SPROUT_AGENT_CONSOLE", "1")

	cmd := &SessionsCommand{}

	output := captureOutput(func() {
		err := cmd.Execute([]string{}, testAgent)
		assert.NoError(t, err)
	})

	// Verify all sessions are listed
	assert.Contains(t, output, "session-001")
	assert.Contains(t, output, "session-002")
	assert.Contains(t, output, "session-003")
}

// TestSessionsCommand_SessionWithMessages tests loading a session with messages
func TestSessionsCommand_SessionWithMessages(t *testing.T) {
	// Create a temp state directory
	tempStateDir := t.TempDir()

	// Override getStateDirFunc to use temp directory
	originalGetStateDirFunc := agent.SetGetStateDirForTest(tempStateDir)
	defer agent.SetGetStateDirFunc(originalGetStateDirFunc)

	// Get the actual working directory
	workingDir, err := os.Getwd()
	require.NoError(t, err)
	sessionID := "msg-session"

	// Create a session with multiple messages
	createSessionFile(t, tempStateDir, sessionID, workingDir, "Message Session")

	// Load and add messages
	scopeHash := hashWorkingDir(workingDir)
	sessionFile := filepath.Join(tempStateDir, "scoped", scopeHash, fmt.Sprintf("session_%s.json", sessionID))
	data, err := os.ReadFile(sessionFile)
	require.NoError(t, err)

	var state agent.ConversationState
	err = json.Unmarshal(data, &state)
	require.NoError(t, err)

	// Add 7 messages (more than the 5 shown in preview)
	for i := 1; i <= 7; i++ {
		state.Messages = append(state.Messages, api.Message{
			Role:    "user",
			Content: fmt.Sprintf("Message %d", i),
		})
		if i < 7 {
			state.Messages = append(state.Messages, api.Message{
				Role:    "assistant",
				Content: fmt.Sprintf("Response %d", i),
			})
		}
	}

	updatedData, err := json.MarshalIndent(state, "", "  ")
	require.NoError(t, err)
	err = os.WriteFile(sessionFile, updatedData, 0600)
	require.NoError(t, err)

	// Create test agent
	testAgent := newIsolatedTestAgent(t)
	defer testAgent.Shutdown()

	cmd := &SessionsCommand{}

	output := captureOutput(func() {
		err := cmd.Execute([]string{"1"}, testAgent)
		assert.NoError(t, err)
	})

	// Verify session loaded
	assert.Contains(t, output, "[ok] Conversation session loaded")

	// Verify all messages were loaded (not just the preview)
	messages := testAgent.GetMessages()
	assert.Len(t, messages, 13) // 7 user + 6 assistant messages
}

// TestSessionsCommand_SessionWithLongContent tests handling of long message content in preview
func TestSessionsCommand_SessionWithLongContent(t *testing.T) {
	// Create a temp state directory
	tempStateDir := t.TempDir()

	// Override getStateDirFunc to use temp directory
	originalGetStateDirFunc := agent.SetGetStateDirForTest(tempStateDir)
	defer agent.SetGetStateDirFunc(originalGetStateDirFunc)

	// Get the actual working directory
	workingDir, err := os.Getwd()
	require.NoError(t, err)
	sessionID := "long-content-session"

	// Create a session with long content
	createSessionFile(t, tempStateDir, sessionID, workingDir, "Long Content Session")

	// Load and add a long message
	scopeHash := hashWorkingDir(workingDir)
	sessionFile := filepath.Join(tempStateDir, "scoped", scopeHash, fmt.Sprintf("session_%s.json", sessionID))
	data, err := os.ReadFile(sessionFile)
	require.NoError(t, err)

	var state agent.ConversationState
	err = json.Unmarshal(data, &state)
	require.NoError(t, err)

	// Add a very long user message
	longContent := strings.Repeat("This is a long message. ", 20) // ~400 characters
	state.Messages = []api.Message{
		{Role: "user", Content: longContent},
	}

	updatedData, err := json.MarshalIndent(state, "", "  ")
	require.NoError(t, err)
	err = os.WriteFile(sessionFile, updatedData, 0600)
	require.NoError(t, err)

	// Create test agent
	testAgent := newIsolatedTestAgent(t)
	defer testAgent.Shutdown()

	cmd := &SessionsCommand{}

	output := captureOutput(func() {
		err := cmd.Execute([]string{"1"}, testAgent)
		assert.NoError(t, err)
	})

	// Verify session loaded
	assert.Contains(t, output, "[ok] Conversation session loaded")

	// Verify the full message was loaded
	messages := testAgent.GetMessages()
	assert.Len(t, messages, 1)
	assert.Equal(t, longContent, messages[0].Content)
}

// TestSessionsCommand_SessionWithMultilineContent tests handling of multiline messages
func TestSessionsCommand_SessionWithMultilineContent(t *testing.T) {
	// Create a temp state directory
	tempStateDir := t.TempDir()

	// Override getStateDirFunc to use temp directory
	originalGetStateDirFunc := agent.SetGetStateDirForTest(tempStateDir)
	defer agent.SetGetStateDirFunc(originalGetStateDirFunc)

	// Get the actual working directory
	workingDir, err := os.Getwd()
	require.NoError(t, err)
	sessionID := "multiline-session"

	// Create a session with multiline content
	createSessionFile(t, tempStateDir, sessionID, workingDir, "Multiline Session")

	// Load and add multiline message
	scopeHash := hashWorkingDir(workingDir)
	sessionFile := filepath.Join(tempStateDir, "scoped", scopeHash, fmt.Sprintf("session_%s.json", sessionID))
	data, err := os.ReadFile(sessionFile)
	require.NoError(t, err)

	var state agent.ConversationState
	err = json.Unmarshal(data, &state)
	require.NoError(t, err)

	// Add multiline content
	multilineContent := "Line 1\nLine 2\nLine 3\nLine 4\nLine 5"
	state.Messages = []api.Message{
		{Role: "user", Content: multilineContent},
	}

	updatedData, err := json.MarshalIndent(state, "", "  ")
	require.NoError(t, err)
	err = os.WriteFile(sessionFile, updatedData, 0600)
	require.NoError(t, err)

	// Create test agent
	testAgent := newIsolatedTestAgent(t)
	defer testAgent.Shutdown()

	cmd := &SessionsCommand{}

	output := captureOutput(func() {
		err := cmd.Execute([]string{"1"}, testAgent)
		assert.NoError(t, err)
	})

	// Verify session loaded
	assert.Contains(t, output, "[ok] Conversation session loaded")

	// Verify the multiline message was loaded correctly
	messages := testAgent.GetMessages()
	assert.Len(t, messages, 1)
	assert.Equal(t, multilineContent, messages[0].Content)
}

// Helper function to parse time for tests
func agentTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		// If parsing fails, return a default time
		return time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	}
	return t
}

// TestSessionsCommand_SessionWithName tests session with custom name
func TestSessionsCommand_SessionWithName(t *testing.T) {
	// Create a temp state directory
	tempStateDir := t.TempDir()

	// Override getStateDirFunc to use temp directory
	originalGetStateDirFunc := agent.SetGetStateDirForTest(tempStateDir)
	defer agent.SetGetStateDirFunc(originalGetStateDirFunc)

	// Get the actual working directory
	workingDir, err := os.Getwd()
	require.NoError(t, err)

	// Create sessions with custom names
	createSessionFile(t, tempStateDir, "session-001", workingDir, "My Project Setup")
	createSessionFile(t, tempStateDir, "session-002", workingDir, "")
	createSessionFile(t, tempStateDir, "session-003", workingDir, "Bug Fix Implementation")

	// Create test agent
	testAgent := newIsolatedTestAgent(t)
	defer testAgent.Shutdown()

	// Set AGENT_CONSOLE to see the list
	// IMPORTANT: Set env var before Execute is called
	// GetEnvSimple("AGENT_CONSOLE") checks SPROUT_AGENT_CONSOLE and LEDIT_AGENT_CONSOLE
	t.Setenv("SPROUT_AGENT_CONSOLE", "1")

	cmd := &SessionsCommand{}

	output := captureOutput(func() {
		err := cmd.Execute([]string{}, testAgent)
		assert.NoError(t, err)
	})

	// Verify sessions are listed
	assert.Contains(t, output, "session-001")
	assert.Contains(t, output, "session-002")
	assert.Contains(t, output, "session-003")
}
