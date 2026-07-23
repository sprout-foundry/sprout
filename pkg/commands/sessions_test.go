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

	"github.com/creack/pty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// Test helper to create an isolated test agent
func newIsolatedTestAgent(t *testing.T) *agent.Agent {
	t.Helper()

	configDir := t.TempDir() + "/.sprout"

	// Set both env vars for isolation (NewAgentWithModel reads SPROUT_CONFIG/SPROUT_CONFIG
	// to determine the config directory; the env vars ensure the agent uses our temp dir)
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
	// GetEnvSimple("AGENT_CONSOLE") checks SPROUT_AGENT_CONSOLE and SPROUT_AGENT_CONSOLE
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
	assert.Contains(t, output, "You: First message")
	assert.Contains(t, output, "Assistant: First response")
	assert.Contains(t, output, "You: Second message")
	assert.Contains(t, output, "Assistant: Second response")
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
	// GetEnvSimple("AGENT_CONSOLE") checks SPROUT_AGENT_CONSOLE and SPROUT_AGENT_CONSOLE
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
	// GetEnvSimple("AGENT_CONSOLE") checks SPROUT_AGENT_CONSOLE and SPROUT_AGENT_CONSOLE
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

// --- PTY-based interactive terminal tests ---

// setupPTY creates a PTY pair and replaces os.Stdin with the slave side.
// Returns a cleanup function that restores stdin and closes both sides.
func setupPTY(t *testing.T) (master *os.File, cleanup func()) {
	t.Helper()

	ptmx, pts, err := pty.Open()
	require.NoError(t, err, "failed to open pty")

	oldStdin := os.Stdin
	os.Stdin = pts

	cleanup = func() {
		os.Stdin = oldStdin
		pts.Close()
		ptmx.Close()
	}
	return ptmx, cleanup
}

// TestSessionsCommand_selectSessionWithDropdown_Interactive_Success tests
// the interactive terminal path where the user selects a valid session.
func TestSessionsCommand_selectSessionWithDropdown_Interactive_Success(t *testing.T) {
	// Create a temp state directory
	tempStateDir := t.TempDir()

	// Override getStateDirFunc to use temp directory
	originalGetStateDirFunc := agent.SetGetStateDirForTest(tempStateDir)
	defer agent.SetGetStateDirFunc(originalGetStateDirFunc)

	// Get the actual working directory
	workingDir, err := os.Getwd()
	require.NoError(t, err)

	sessionID := "interactive-session"
	createSessionFile(t, tempStateDir, sessionID, workingDir, "Interactive Session")

	// Load the session file and add messages
	scopeHash := hashWorkingDir(workingDir)
	sessionFile := filepath.Join(tempStateDir, "scoped", scopeHash, fmt.Sprintf("session_%s.json", sessionID))
	data, err := os.ReadFile(sessionFile)
	require.NoError(t, err)

	var state agent.ConversationState
	err = json.Unmarshal(data, &state)
	require.NoError(t, err)

	state.Messages = []api.Message{
		{Role: "user", Content: "Hello from interactive"},
		{Role: "assistant", Content: "Interactive response"},
	}

	updatedData, err := json.MarshalIndent(state, "", "  ")
	require.NoError(t, err)
	err = os.WriteFile(sessionFile, updatedData, 0600)
	require.NoError(t, err)

	// Create test agent
	testAgent := newIsolatedTestAgent(t)
	defer testAgent.Shutdown()

	// Ensure agent console is OFF
	t.Setenv("SPROUT_AGENT_CONSOLE", "")

	// Setup PTY
	ptmx, cleanup := setupPTY(t)
	defer cleanup()

	// Build sessions list (newest first — only one session)
	sessions := []agent.SessionInfo{
		{
			SessionID:        sessionID,
			Name:             "Interactive Session",
			LastUpdated:      agentTime("2024-01-15T10:00:00Z"),
			WorkingDirectory: workingDir,
		},
	}

	cmd := &SessionsCommand{}

	// Write "1\n" to select the first session
	_, err = ptmx.Write([]byte("1\n"))
	require.NoError(t, err)

	output := captureOutput(func() {
		err = cmd.selectSessionWithDropdown(sessions, testAgent)
	})

	// Verify success
	assert.NoError(t, err)
	assert.Contains(t, output, "Conversation session loaded")
	assert.Contains(t, output, sessionID)

	// Verify messages were loaded
	messages := testAgent.GetMessages()
	assert.Len(t, messages, 2)
	assert.Equal(t, "Hello from interactive", messages[0].Content)
	assert.Equal(t, "Interactive response", messages[1].Content)
}

// TestSessionsCommand_selectSessionWithDropdown_Interactive_Cancel tests
// the interactive terminal path where the user cancels by entering 0.
func TestSessionsCommand_selectSessionWithDropdown_Interactive_Cancel(t *testing.T) {
	// Create a temp state directory
	tempStateDir := t.TempDir()

	// Override getStateDirFunc to use temp directory
	originalGetStateDirFunc := agent.SetGetStateDirForTest(tempStateDir)
	defer agent.SetGetStateDirFunc(originalGetStateDirFunc)

	workingDir, err := os.Getwd()
	require.NoError(t, err)

	sessionID := "cancel-session"
	createSessionFile(t, tempStateDir, sessionID, workingDir, "Cancel Session")

	// Create test agent
	testAgent := newIsolatedTestAgent(t)
	defer testAgent.Shutdown()

	// Ensure agent console is OFF
	t.Setenv("SPROUT_AGENT_CONSOLE", "")

	// Setup PTY
	ptmx, cleanup := setupPTY(t)
	defer cleanup()

	sessions := []agent.SessionInfo{
		{
			SessionID:        sessionID,
			Name:             "Cancel Session",
			LastUpdated:      agentTime("2024-01-15T10:00:00Z"),
			WorkingDirectory: workingDir,
		},
	}

	cmd := &SessionsCommand{}

	// Write "0\n" to cancel
	_, err = ptmx.Write([]byte("0\n"))
	require.NoError(t, err)

	output := captureOutput(func() {
		err = cmd.selectSessionWithDropdown(sessions, testAgent)
	})

	// Verify no error and cancellation message
	assert.NoError(t, err)
	assert.Contains(t, output, "Cancelled.")

	// Verify no messages were loaded
	messages := testAgent.GetMessages()
	assert.Empty(t, messages)
}

// TestSessionsCommand_selectSessionWithDropdown_Interactive_InvalidInput tests
// the interactive terminal path where the user enters non-numeric input.
func TestSessionsCommand_selectSessionWithDropdown_Interactive_InvalidInput(t *testing.T) {
	// Create a temp state directory
	tempStateDir := t.TempDir()

	// Override getStateDirFunc to use temp directory
	originalGetStateDirFunc := agent.SetGetStateDirForTest(tempStateDir)
	defer agent.SetGetStateDirFunc(originalGetStateDirFunc)

	workingDir, err := os.Getwd()
	require.NoError(t, err)

	sessionID := "invalid-input-session"
	createSessionFile(t, tempStateDir, sessionID, workingDir, "Invalid Input Session")

	// Create test agent
	testAgent := newIsolatedTestAgent(t)
	defer testAgent.Shutdown()

	// Ensure agent console is OFF
	t.Setenv("SPROUT_AGENT_CONSOLE", "")

	// Setup PTY
	ptmx, cleanup := setupPTY(t)
	defer cleanup()

	sessions := []agent.SessionInfo{
		{
			SessionID:        sessionID,
			Name:             "Invalid Input Session",
			LastUpdated:      agentTime("2024-01-15T10:00:00Z"),
			WorkingDirectory: workingDir,
		},
	}

	cmd := &SessionsCommand{}

	// Write "abc\n" — invalid non-numeric input
	_, err = ptmx.Write([]byte("abc\n"))
	require.NoError(t, err)

	output := captureOutput(func() {
		err = cmd.selectSessionWithDropdown(sessions, testAgent)
	})

	// PromptForSelection returns (0, false) for invalid input,
	// and selectSessionWithDropdown returns nil in that case
	assert.NoError(t, err)
	assert.Contains(t, output, "Invalid input")

	// Verify no messages were loaded
	messages := testAgent.GetMessages()
	assert.Empty(t, messages)
}

// TestSessionsCommand_selectSessionWithDropdown_Interactive_OutOfRange tests
// the interactive terminal path where the user enters an out-of-range number.
func TestSessionsCommand_selectSessionWithDropdown_Interactive_OutOfRange(t *testing.T) {
	// Create a temp state directory
	tempStateDir := t.TempDir()

	// Override getStateDirFunc to use temp directory
	originalGetStateDirFunc := agent.SetGetStateDirForTest(tempStateDir)
	defer agent.SetGetStateDirFunc(originalGetStateDirFunc)

	workingDir, err := os.Getwd()
	require.NoError(t, err)

	sessionID := "outofrange-session"
	createSessionFile(t, tempStateDir, sessionID, workingDir, "Out of Range Session")

	// Create test agent
	testAgent := newIsolatedTestAgent(t)
	defer testAgent.Shutdown()

	// Ensure agent console is OFF
	t.Setenv("SPROUT_AGENT_CONSOLE", "")

	// Setup PTY
	ptmx, cleanup := setupPTY(t)
	defer cleanup()

	sessions := []agent.SessionInfo{
		{
			SessionID:        sessionID,
			Name:             "Out of Range Session",
			LastUpdated:      agentTime("2024-01-15T10:00:00Z"),
			WorkingDirectory: workingDir,
		},
	}

	cmd := &SessionsCommand{}

	// Write "99\n" — out of range (only 1 session exists)
	_, err = ptmx.Write([]byte("99\n"))
	require.NoError(t, err)

	output := captureOutput(func() {
		err = cmd.selectSessionWithDropdown(sessions, testAgent)
	})

	// PromptForSelection returns (0, false) for out-of-range,
	// and selectSessionWithDropdown returns nil in that case
	assert.NoError(t, err)
	assert.Contains(t, output, "Invalid selection")

	// Verify no messages were loaded
	messages := testAgent.GetMessages()
	assert.Empty(t, messages)
}

// TestSessionsCommand_selectSessionWithDropdown_Interactive_LoadError tests
// the interactive terminal path where LoadStateScoped fails.
func TestSessionsCommand_selectSessionWithDropdown_Interactive_LoadError(t *testing.T) {
	// Create a temp state directory
	tempStateDir := t.TempDir()

	// Override getStateDirFunc to use temp directory
	originalGetStateDirFunc := agent.SetGetStateDirForTest(tempStateDir)
	defer agent.SetGetStateDirFunc(originalGetStateDirFunc)

	workingDir, err := os.Getwd()
	require.NoError(t, err)

	// Create test agent
	testAgent := newIsolatedTestAgent(t)
	defer testAgent.Shutdown()

	// Ensure agent console is OFF
	t.Setenv("SPROUT_AGENT_CONSOLE", "")

	// Setup PTY
	ptmx, cleanup := setupPTY(t)
	defer cleanup()

	// Pass a session that doesn't have a backing file in the state dir
	sessions := []agent.SessionInfo{
		{
			SessionID:        "nonexistent-session",
			Name:             "Nonexistent Session",
			LastUpdated:      agentTime("2024-01-15T10:00:00Z"),
			WorkingDirectory: workingDir,
		},
	}

	cmd := &SessionsCommand{}

	// Write "1\n" to select the session
	_, err = ptmx.Write([]byte("1\n"))
	require.NoError(t, err)

	err = cmd.selectSessionWithDropdown(sessions, testAgent)

	// Should get an error because the session file doesn't exist
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load session")
}

// TestSessionsCommand_selectSessionWithDropdown_Interactive_EmptyNameFallback tests
// the interactive terminal path where session Name is empty and
// GetSessionPreviewScoped is used as fallback.
func TestSessionsCommand_selectSessionWithDropdown_Interactive_EmptyNameFallback(t *testing.T) {
	// Create a temp state directory
	tempStateDir := t.TempDir()

	// Override getStateDirFunc to use temp directory
	originalGetStateDirFunc := agent.SetGetStateDirForTest(tempStateDir)
	defer agent.SetGetStateDirFunc(originalGetStateDirFunc)

	workingDir, err := os.Getwd()
	require.NoError(t, err)

	sessionID := "no-name-session"

	// Create session file with empty name but with a user message (for preview)
	scopeHash := hashWorkingDir(workingDir)
	scopeDir := filepath.Join(tempStateDir, "scoped", scopeHash)
	require.NoError(t, os.MkdirAll(scopeDir, 0700))

	state := &agent.ConversationState{
		SessionID:        sessionID,
		Name:             "", // Empty name — should trigger GetSessionPreviewScoped fallback
		WorkingDirectory: workingDir,
		Messages: []api.Message{
			{Role: "user", Content: "This is a preview message for testing the fallback"},
		},
	}

	data, err := json.MarshalIndent(state, "", "  ")
	require.NoError(t, err)

	sessionFile := filepath.Join(scopeDir, fmt.Sprintf("session_%s.json", sessionID))
	require.NoError(t, os.WriteFile(sessionFile, data, 0600))

	// Create test agent
	testAgent := newIsolatedTestAgent(t)
	defer testAgent.Shutdown()

	// Ensure agent console is OFF
	t.Setenv("SPROUT_AGENT_CONSOLE", "")

	// Setup PTY
	ptmx, cleanup := setupPTY(t)
	defer cleanup()

	sessions := []agent.SessionInfo{
		{
			SessionID:        sessionID,
			Name:             "", // Empty name
			LastUpdated:      agentTime("2024-01-15T10:00:00Z"),
			WorkingDirectory: workingDir,
		},
	}

	cmd := &SessionsCommand{}

	// Write "1\n" to select the session
	_, err = ptmx.Write([]byte("1\n"))
	require.NoError(t, err)

	output := captureOutput(func() {
		err = cmd.selectSessionWithDropdown(sessions, testAgent)
	})

	// Verify success
	assert.NoError(t, err)
	assert.Contains(t, output, "Conversation session loaded")
	assert.Contains(t, output, sessionID)

	// Verify the preview was shown (first 50 chars of first user message)
	// The preview should contain "This is a preview message for testing the fallback"
	// truncated to 50 chars with "..."
	assert.Contains(t, output, "This is a preview message for testing the fallback")
}

// TestSessionsCommand_selectSessionWithDropdown_Interactive_MultipleSessions_SelectSecond tests
// selecting the second session from a list of three.
func TestSessionsCommand_selectSessionWithDropdown_Interactive_MultipleSessions_SelectSecond(t *testing.T) {
	// Create a temp state directory
	tempStateDir := t.TempDir()

	// Override getStateDirFunc to use temp directory
	originalGetStateDirFunc := agent.SetGetStateDirForTest(tempStateDir)
	defer agent.SetGetStateDirFunc(originalGetStateDirFunc)

	workingDir, err := os.Getwd()
	require.NoError(t, err)

	// Create 3 sessions with distinct messages
	createSessionFile(t, tempStateDir, "session-one", workingDir, "Session One")
	createSessionFile(t, tempStateDir, "session-two", workingDir, "Session Two")
	createSessionFile(t, tempStateDir, "session-three", workingDir, "Session Three")

	// Add distinct messages to each session
	scopeHash := hashWorkingDir(workingDir)
	scopeDir := filepath.Join(tempStateDir, "scoped", scopeHash)

	for _, sessionData := range []struct {
		id       string
		messages []api.Message
	}{
		{"session-one", []api.Message{{Role: "user", Content: "Message from session one"}}},
		{"session-two", []api.Message{{Role: "user", Content: "Message from session two"}}},
		{"session-three", []api.Message{{Role: "user", Content: "Message from session three"}}},
	} {
		sessionFile := filepath.Join(scopeDir, fmt.Sprintf("session_%s.json", sessionData.id))
		data, err := os.ReadFile(sessionFile)
		require.NoError(t, err)

		var state agent.ConversationState
		err = json.Unmarshal(data, &state)
		require.NoError(t, err)

		state.Messages = sessionData.messages

		updatedData, err := json.MarshalIndent(state, "", "  ")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(sessionFile, updatedData, 0600))
	}

	// Create test agent
	testAgent := newIsolatedTestAgent(t)
	defer testAgent.Shutdown()

	// Ensure agent console is OFF
	t.Setenv("SPROUT_AGENT_CONSOLE", "")

	// Setup PTY
	ptmx, cleanup := setupPTY(t)
	defer cleanup()

	// Sessions in newest-first order (session-one is first, session-two is second)
	sessions := []agent.SessionInfo{
		{
			SessionID:        "session-one",
			Name:             "Session One",
			LastUpdated:      agentTime("2024-01-15T10:00:00Z"),
			WorkingDirectory: workingDir,
		},
		{
			SessionID:        "session-two",
			Name:             "Session Two",
			LastUpdated:      agentTime("2024-01-16T10:00:00Z"),
			WorkingDirectory: workingDir,
		},
		{
			SessionID:        "session-three",
			Name:             "Session Three",
			LastUpdated:      agentTime("2024-01-17T10:00:00Z"),
			WorkingDirectory: workingDir,
		},
	}

	cmd := &SessionsCommand{}

	// Write "2\n" to select the second session
	_, err = ptmx.Write([]byte("2\n"))
	require.NoError(t, err)

	output := captureOutput(func() {
		err = cmd.selectSessionWithDropdown(sessions, testAgent)
	})

	// Verify the second session was loaded
	assert.NoError(t, err)
	assert.Contains(t, output, "Conversation session loaded")
	assert.Contains(t, output, "session-two")

	// Verify the messages match session-two
	messages := testAgent.GetMessages()
	assert.Len(t, messages, 1)
	assert.Equal(t, "Message from session two", messages[0].Content)
}

// TestSessionsCommand_selectSessionWithDropdown_Interactive_NoInput tests
// the interactive terminal path where no input is provided (EOF).
func TestSessionsCommand_selectSessionWithDropdown_Interactive_NoInput(t *testing.T) {
	// Create a temp state directory
	tempStateDir := t.TempDir()

	// Override getStateDirFunc to use temp directory
	originalGetStateDirFunc := agent.SetGetStateDirForTest(tempStateDir)
	defer agent.SetGetStateDirFunc(originalGetStateDirFunc)

	workingDir, err := os.Getwd()
	require.NoError(t, err)

	sessionID := "no-input-session"
	createSessionFile(t, tempStateDir, sessionID, workingDir, "No Input Session")

	// Create test agent
	testAgent := newIsolatedTestAgent(t)
	defer testAgent.Shutdown()

	// Ensure agent console is OFF
	t.Setenv("SPROUT_AGENT_CONSOLE", "")

	// Setup PTY manually (not via setupPTY) because we need to close the
	// master before calling the function to trigger EOF, and we must avoid
	// a double-close on the master fd in cleanup.
	ptmx, pts, ptyErr := pty.Open()
	require.NoError(t, ptyErr, "failed to open pty")
	oldStdin := os.Stdin
	os.Stdin = pts

	sessions := []agent.SessionInfo{
		{
			SessionID:        sessionID,
			Name:             "No Input Session",
			LastUpdated:      agentTime("2024-01-15T10:00:00Z"),
			WorkingDirectory: workingDir,
		},
	}

	cmd := &SessionsCommand{}

	// Close master to send EOF on slave — do this before calling the function
	// so the bufio.Scanner in PromptForSelection sees EOF immediately.
	ptmx.Close()

	_ = captureOutput(func() {
		err = cmd.selectSessionWithDropdown(sessions, testAgent)
	})

	// Restore stdin and close slave (master already closed above — no double-close)
	os.Stdin = oldStdin
	pts.Close()

	// PromptForSelection returns (0, false) on EOF
	// selectSessionWithDropdown returns nil
	assert.NoError(t, err)

	// Verify no messages were loaded
	messages := testAgent.GetMessages()
	assert.Empty(t, messages)
}

// TestSessionsCommand_selectSessionWithDropdown_Interactive_PreviewDisplay tests
// that displayConversationPreview is called after successful interactive selection.
func TestSessionsCommand_selectSessionWithDropdown_Interactive_PreviewDisplay(t *testing.T) {
	// Create a temp state directory
	tempStateDir := t.TempDir()

	// Override getStateDirFunc to use temp directory
	originalGetStateDirFunc := agent.SetGetStateDirForTest(tempStateDir)
	defer agent.SetGetStateDirFunc(originalGetStateDirFunc)

	workingDir, err := os.Getwd()
	require.NoError(t, err)

	sessionID := "preview-session"

	// Create session with multiple messages
	scopeHash := hashWorkingDir(workingDir)
	scopeDir := filepath.Join(tempStateDir, "scoped", scopeHash)
	require.NoError(t, os.MkdirAll(scopeDir, 0700))

	state := &agent.ConversationState{
		SessionID:        sessionID,
		Name:             "Preview Session",
		WorkingDirectory: workingDir,
		Messages: []api.Message{
			{Role: "user", Content: "First user message"},
			{Role: "assistant", Content: "First assistant response"},
			{Role: "user", Content: "Second user message"},
			{Role: "assistant", Content: "Second assistant response"},
			{Role: "user", Content: "Third user message"},
		},
	}

	data, err := json.MarshalIndent(state, "", "  ")
	require.NoError(t, err)

	sessionFile := filepath.Join(scopeDir, fmt.Sprintf("session_%s.json", sessionID))
	require.NoError(t, os.WriteFile(sessionFile, data, 0600))

	// Create test agent
	testAgent := newIsolatedTestAgent(t)
	defer testAgent.Shutdown()

	// Ensure agent console is OFF
	t.Setenv("SPROUT_AGENT_CONSOLE", "")

	// Setup PTY
	ptmx, cleanup := setupPTY(t)
	defer cleanup()

	sessions := []agent.SessionInfo{
		{
			SessionID:        sessionID,
			Name:             "Preview Session",
			LastUpdated:      agentTime("2024-01-15T10:00:00Z"),
			WorkingDirectory: workingDir,
		},
	}

	cmd := &SessionsCommand{}

	// Write "1\n" to select the session
	_, err = ptmx.Write([]byte("1\n"))
	require.NoError(t, err)

	output := captureOutput(func() {
		err = cmd.selectSessionWithDropdown(sessions, testAgent)
	})

	// Verify success
	assert.NoError(t, err)
	assert.Contains(t, output, "Conversation session loaded")

	// Verify preview section is shown
	assert.Contains(t, output, "Recent conversation preview")
	assert.Contains(t, output, "You:")
	assert.Contains(t, output, "Assistant:")
}

// TestSessionsCommand_selectSessionWithDropdown_Interactive_PreviewDisplay_NoMessages tests
// that displayConversationPreview is called but shows nothing when there are no messages.
func TestSessionsCommand_selectSessionWithDropdown_Interactive_PreviewDisplay_NoMessages(t *testing.T) {
	// Create a temp state directory
	tempStateDir := t.TempDir()

	// Override getStateDirFunc to use temp directory
	originalGetStateDirFunc := agent.SetGetStateDirForTest(tempStateDir)
	defer agent.SetGetStateDirFunc(originalGetStateDirFunc)

	workingDir, err := os.Getwd()
	require.NoError(t, err)

	sessionID := "empty-preview-session"

	// Create session with NO messages
	scopeHash := hashWorkingDir(workingDir)
	scopeDir := filepath.Join(tempStateDir, "scoped", scopeHash)
	require.NoError(t, os.MkdirAll(scopeDir, 0700))

	state := &agent.ConversationState{
		SessionID:        sessionID,
		Name:             "Empty Preview Session",
		WorkingDirectory: workingDir,
		Messages:         []api.Message{},
	}

	data, err := json.MarshalIndent(state, "", "  ")
	require.NoError(t, err)

	sessionFile := filepath.Join(scopeDir, fmt.Sprintf("session_%s.json", sessionID))
	require.NoError(t, os.WriteFile(sessionFile, data, 0600))

	// Create test agent
	testAgent := newIsolatedTestAgent(t)
	defer testAgent.Shutdown()

	// Ensure agent console is OFF
	t.Setenv("SPROUT_AGENT_CONSOLE", "")

	// Setup PTY
	ptmx, cleanup := setupPTY(t)
	defer cleanup()

	sessions := []agent.SessionInfo{
		{
			SessionID:        sessionID,
			Name:             "Empty Preview Session",
			LastUpdated:      agentTime("2024-01-15T10:00:00Z"),
			WorkingDirectory: workingDir,
		},
	}

	cmd := &SessionsCommand{}

	// Write "1\n" to select the session
	_, err = ptmx.Write([]byte("1\n"))
	require.NoError(t, err)

	output := captureOutput(func() {
		err = cmd.selectSessionWithDropdown(sessions, testAgent)
	})

	// Verify success
	assert.NoError(t, err)
	assert.Contains(t, output, "Conversation session loaded")

	// Verify no preview section (no messages to show)
	assert.NotContains(t, output, "Recent conversation preview")
}

// TestSessionsCommand_selectSessionWithDropdown_AgentConsole_WithPreview tests
// the agent console mode where session has no name and we should see the SessionID.
func TestSessionsCommand_selectSessionWithDropdown_AgentConsole_WithPreview(t *testing.T) {
	// Create a temp state directory
	tempStateDir := t.TempDir()

	// Override getStateDirFunc to use temp directory
	originalGetStateDirFunc := agent.SetGetStateDirForTest(tempStateDir)
	defer agent.SetGetStateDirFunc(originalGetStateDirFunc)

	workingDir, err := os.Getwd()
	require.NoError(t, err)

	sessionID := "console-preview-session"

	// Create session with empty name but with a user message
	scopeHash := hashWorkingDir(workingDir)
	scopeDir := filepath.Join(tempStateDir, "scoped", scopeHash)
	require.NoError(t, os.MkdirAll(scopeDir, 0700))

	state := &agent.ConversationState{
		SessionID:        sessionID,
		Name:             "", // Empty name
		WorkingDirectory: workingDir,
		Messages: []api.Message{
			{Role: "user", Content: "Preview text for console mode"},
		},
	}

	data, err := json.MarshalIndent(state, "", "  ")
	require.NoError(t, err)

	sessionFile := filepath.Join(scopeDir, fmt.Sprintf("session_%s.json", sessionID))
	require.NoError(t, os.WriteFile(sessionFile, data, 0600))

	// Create test agent
	testAgent := newIsolatedTestAgent(t)
	defer testAgent.Shutdown()

	// Set AGENT_CONSOLE
	t.Setenv("SPROUT_AGENT_CONSOLE", "1")

	cmd := &SessionsCommand{}

	// In agent console mode, sessions are listed with SessionID regardless of name
	sessions := []agent.SessionInfo{
		{
			SessionID:        sessionID,
			Name:             "", // Empty name
			LastUpdated:      agentTime("2024-01-15T10:00:00Z"),
			WorkingDirectory: workingDir,
		},
	}

	output := captureOutput(func() {
		err = cmd.selectSessionWithDropdown(sessions, testAgent)
	})

	// Verify output contains the session ID
	assert.NoError(t, err)
	assert.Contains(t, output, "Available Sessions:")
	assert.Contains(t, output, sessionID)
}

// TestSessionsCommand_Execute_WithArgs_MultipleSessions_SelectSecond tests
// direct loading of the second session from multiple sessions.
func TestSessionsCommand_Execute_WithArgs_MultipleSessions_SelectSecond(t *testing.T) {
	// Create a temp state directory
	tempStateDir := t.TempDir()

	// Override getStateDirFunc to use temp directory
	originalGetStateDirFunc := agent.SetGetStateDirForTest(tempStateDir)
	defer agent.SetGetStateDirFunc(originalGetStateDirFunc)

	workingDir, err := os.Getwd()
	require.NoError(t, err)

	// Create 3 sessions with distinct messages
	createSessionFile(t, tempStateDir, "session-one", workingDir, "Session One")
	createSessionFile(t, tempStateDir, "session-two", workingDir, "Session Two")
	createSessionFile(t, tempStateDir, "session-three", workingDir, "Session Three")

	// Add distinct messages to each session and set explicit timestamps
	// to ensure deterministic ordering (newest-first)
	scopeHash := hashWorkingDir(workingDir)
	scopeDir := filepath.Join(tempStateDir, "scoped", scopeHash)

	sessions := []struct {
		id       string
		messages []api.Message
		modTime  time.Time // explicit mod time to ensure deterministic sort order
	}{
		{"session-one", []api.Message{{Role: "user", Content: "One"}}, time.Date(2024, 1, 10, 10, 0, 0, 0, time.UTC)},
		{"session-two", []api.Message{{Role: "user", Content: "Two"}}, time.Date(2024, 1, 12, 10, 0, 0, 0, time.UTC)},
		{"session-three", []api.Message{{Role: "user", Content: "Three"}}, time.Date(2024, 1, 14, 10, 0, 0, 0, time.UTC)},
	}

	for _, sessionData := range sessions {
		sessionFile := filepath.Join(scopeDir, fmt.Sprintf("session_%s.json", sessionData.id))
		data, err := os.ReadFile(sessionFile)
		require.NoError(t, err)

		var state agent.ConversationState
		err = json.Unmarshal(data, &state)
		require.NoError(t, err)

		state.Messages = sessionData.messages

		updatedData, err := json.MarshalIndent(state, "", "  ")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(sessionFile, updatedData, 0600))
		require.NoError(t, os.Chtimes(sessionFile, sessionData.modTime, sessionData.modTime))
	}

	// Create test agent
	testAgent := newIsolatedTestAgent(t)
	defer testAgent.Shutdown()

	cmd := &SessionsCommand{}

	output := captureOutput(func() {
		err := cmd.Execute([]string{"2"}, testAgent)
		assert.NoError(t, err)
	})

	// Verify the second session was loaded
	assert.Contains(t, output, "[ok] Conversation session loaded")

	// Sessions are sorted newest-first:
	// 1 = session-three (Jan 14), 2 = session-two (Jan 12), 3 = session-one (Jan 10)
	// So selecting "2" should load "session-two" with content "Two"
	messages := testAgent.GetMessages()
	assert.Len(t, messages, 1)
	assert.Equal(t, "Two", messages[0].Content)
}

// TestSessionsCommand_selectSessionWithDropdown_Interactive_NegativeInput tests
// the interactive terminal path where the user enters a negative number.
func TestSessionsCommand_selectSessionWithDropdown_Interactive_NegativeInput(t *testing.T) {
	// Create a temp state directory
	tempStateDir := t.TempDir()

	// Override getStateDirFunc to use temp directory
	originalGetStateDirFunc := agent.SetGetStateDirForTest(tempStateDir)
	defer agent.SetGetStateDirFunc(originalGetStateDirFunc)

	workingDir, err := os.Getwd()
	require.NoError(t, err)

	sessionID := "negative-session"
	createSessionFile(t, tempStateDir, sessionID, workingDir, "Negative Session")

	// Create test agent
	testAgent := newIsolatedTestAgent(t)
	defer testAgent.Shutdown()

	// Ensure agent console is OFF
	t.Setenv("SPROUT_AGENT_CONSOLE", "")

	// Setup PTY
	ptmx, cleanup := setupPTY(t)
	defer cleanup()

	sessions := []agent.SessionInfo{
		{
			SessionID:        sessionID,
			Name:             "Negative Session",
			LastUpdated:      agentTime("2024-01-15T10:00:00Z"),
			WorkingDirectory: workingDir,
		},
	}

	cmd := &SessionsCommand{}

	// Write "-1\n" — negative number, out of range
	_, err = ptmx.Write([]byte("-1\n"))
	require.NoError(t, err)

	output := captureOutput(func() {
		err = cmd.selectSessionWithDropdown(sessions, testAgent)
	})

	// PromptForSelection returns (0, false) for out-of-range negative
	assert.NoError(t, err)
	assert.Contains(t, output, "Invalid selection")

	// Verify no messages were loaded
	messages := testAgent.GetMessages()
	assert.Empty(t, messages)
}
