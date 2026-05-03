package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSessionInfo_FileNotFound(t *testing.T) {
	// Use a session ID that doesn't exist
	_, err := LoadSessionInfo("nonexistent-session-id-12345")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
}

func TestLoadSessionInfo_BadJSON(t *testing.T) {
	// Override getStateDirFunc to use a temp dir
	tmpDir := t.TempDir()
	testStateDir := filepath.Join(tmpDir, "sessions")
	os.MkdirAll(testStateDir, 0700)

	getStateDirFunc = func() (string, error) {
		return testStateDir, nil
	}
	defer func() { getStateDirFunc = defaultGetStateDir }()

	// Write a file with invalid JSON
	stateFile := filepath.Join(testStateDir, "session_bad-json.json")
	if err := os.WriteFile(stateFile, []byte("{invalid json"), 0600); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	_, err := LoadSessionInfo("bad-json")
	if err == nil {
		t.Fatal("expected error for bad JSON")
	}
}

func TestLoadSessionInfo_ValidSession(t *testing.T) {
	tmpDir := t.TempDir()
	testStateDir := filepath.Join(tmpDir, "sessions")
	os.MkdirAll(testStateDir, 0700)

	getStateDirFunc = func() (string, error) {
		return testStateDir, nil
	}
	defer func() { getStateDirFunc = defaultGetStateDir }()

	// Write a valid JSON session file
	stateFile := filepath.Join(testStateDir, "session_test-session.json")
	validJSON := `{"session_id":"test-session","name":"Test Session","working_directory":"/tmp/test","total_tokens":100,"total_cost":0.01}`
	if err := os.WriteFile(stateFile, []byte(validJSON), 0600); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	state, err := LoadSessionInfo("test-session")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if state.SessionID != "test-session" {
		t.Errorf("expected session ID 'test-session', got %q", state.SessionID)
	}
	if state.Name != "Test Session" {
		t.Errorf("expected name 'Test Session', got %q", state.Name)
	}
	if state.TotalTokens != 100 {
		t.Errorf("expected total tokens 100, got %d", state.TotalTokens)
	}
	if state.TotalCost != 0.01 {
		t.Errorf("expected total cost 0.01, got %f", state.TotalCost)
	}
}
