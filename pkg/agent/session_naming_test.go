package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

func TestGenerateSessionName(t *testing.T) {
	tests := []struct {
		name            string
		messages        []api.Message
		expectedName    string
	}{
		{
			name: "first user message as name",
			messages: []api.Message{
				{Role: "system", Content: "You are a helpful assistant."},
				{Role: "user", Content: "Refactor the persistence.go file to improve session naming"},
				{Role: "assistant", Content: "I'll help you with that."},
			},
			expectedName: "Refactor the persistence.go file to improve session naming",
		},
		{
			name: "custom session name override",
			messages: []api.Message{
				{Role: "system", Content: "[SESSION_NAME:]Custom Session Name"},
				{Role: "user", Content: "Some other message"},
			},
			expectedName: "Custom Session Name",
		},
		{
			name: "name with newlines trimmed",
			messages: []api.Message{
				{Role: "user", Content: "First line\n\nSecond line\nThird line"},
			},
			expectedName: "First line Second line Third line",
		},
		{
			name: "long name truncated",
			messages: []api.Message{
				{Role: "user", Content: "This is a very long message that should be truncated to sixty chars and then followed by three dots"},
			},
			expectedName: "This is a very long message that should be truncated to sixt...",
		},
		{
			name: "no user message returns unnamed",
			messages: []api.Message{
				{Role: "system", Content: "System message"},
				{Role: "assistant", Content: "Assistant message"},
			},
			expectedName: "Unnamed session",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := &Agent{
				messages: tt.messages,
			}
			name := agent.generateSessionName()
			if name != tt.expectedName {
				t.Errorf("generateSessionName() = %q, want %q", name, tt.expectedName)
			}
		})
	}
}

func TestSetSessionName(t *testing.T) {
	agent := &Agent{
		messages: []api.Message{
			{Role: "user", Content: "Original message"},
		},
	}

	agent.SetSessionName("Custom Session Name")

	// Check that custom name marker is prepended
	expectedMarker := "[SESSION_NAME:]Custom Session Name"
	found := false
	for _, msg := range agent.messages {
		if msg.Content == expectedMarker {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("SetSessionName() did not add custom name marker")
	}

	// Check that generateSessionName returns the custom name
	name := agent.generateSessionName()
	if name != "Custom Session Name" {
		t.Errorf("generateSessionName() after SetSessionName() = %q, want Custom Session Name", name)
	}
}

func TestSessionPersistenceWithName(t *testing.T) {
	// Create a temp directory for test sessions
	tmpDir := t.TempDir()

	// Override GetStateDir for this test
	originalGetStateDir := getStateDirFunc
	getStateDirFunc = func() (string, error) {
		if err := os.MkdirAll(tmpDir, 0700); err != nil {
			return "", err
		}
		return tmpDir, nil
	}
	defer func() {
		getStateDirFunc = originalGetStateDir
	}()

	agent := &Agent{
		messages: []api.Message{
			{Role: "user", Content: "Test refactoring session"},
			{Role: "assistant", Content: "Here's my plan..."},
		},
		sessionID: "test_session_123",
	}

	// Save session
	stateDir, err := GetStateDir()
	if err != nil {
		t.Fatalf("GetStateDir() failed: %v", err)
	}
	stateFile := filepath.Join(stateDir, "session_test_session_123.json")

	state := ConversationState{
		Messages:    agent.messages,
		SessionID:   agent.sessionID,
		Name:        "Test Refactoring Session",
		LastUpdated: time.Now(),
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent failed: %v", err)
	}

	if err := os.WriteFile(stateFile, data, 0600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Verify name is in session info
	sessions, err := ListSessionsWithTimestamps()
	if err != nil {
		t.Fatalf("ListSessionsWithTimestamps failed: %v", err)
	}

	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}

	if sessions[0].Name != "Test Refactoring Session" {
		t.Errorf("session name = %q, want Test Refactoring Session", sessions[0].Name)
	}
}
