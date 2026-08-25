package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSessionInfo(t *testing.T) {
	// Not parallel: modifies package-level getStateDirFunc
	tmpDir := t.TempDir()
	stateDir := filepath.Join(tmpDir, "sessions")
	getStateDirFunc = func() (string, error) {
		return stateDir, nil
	}
	defer func() {
		getStateDirFunc = defaultGetStateDir
	}()

	t.Run("non-existent session returns error", func(t *testing.T) {
		_, err := LoadSessionInfo("non-existent-session-id")
		if err == nil {
			t.Error("LoadSessionInfo should return error for non-existent session")
		}
	})

	t.Run("non-existent session file error message", func(t *testing.T) {
		_, err := LoadSessionInfo("also-missing")
		if err == nil {
			t.Error("should return error")
		}
	})

	// Create a valid session file and test loading
	t.Run("valid session file loads", func(t *testing.T) {
		// Create the session directory
		scopedDir := filepath.Join(stateDir, "scoped")
		if err := os.MkdirAll(scopedDir, 0700); err != nil {
			t.Fatal(err)
		}

		// Compute the scope hash for the working dir
		cwd, _ := os.Getwd()
		scopeHash := workingDirectoryScopeHash(cwd)
		scopeDir := filepath.Join(scopedDir, scopeHash)
		if err := os.MkdirAll(scopeDir, 0700); err != nil {
			t.Fatal(err)
		}

		sessionFile := filepath.Join(scopeDir, "session_test-session.json")
		// Write a minimal valid session file
		content := `{
			"messages": [],
			"session_id": "test-session",
			"working_directory": "` + cwd + `"
		}`
		if err := os.WriteFile(sessionFile, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}

		state, err := LoadSessionInfo("test-session")
		if err != nil {
			t.Fatalf("LoadSessionInfo returned error: %v", err)
		}
		if state == nil {
			t.Fatal("LoadSessionInfo returned nil state")
		}
		if state.SessionID != "test-session" {
			t.Errorf("SessionID = %q, want %q", state.SessionID, "test-session")
		}
	})
}

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
