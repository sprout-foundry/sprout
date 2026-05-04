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
