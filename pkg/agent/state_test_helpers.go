package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// SetStateDirFuncForTesting replaces the internal GetStateDir implementation for tests.
// It returns a restore function that will reset the original implementation when called.
// This function is safe for use in test code only.
//
// Usage:
//
//	restore := agent.SetStateDirFuncForTesting(func() (string, error) {
//	    return t.TempDir(), nil
//	})
//	defer restore()
func SetStateDirFuncForTesting(fn func() (string, error)) func() {
	original := getStateDirFunc
	getStateDirFunc = fn
	return func() { getStateDirFunc = original }
}

// BuildScopedSessionPathForTesting constructs the scoped session file path for test setup.
func BuildScopedSessionPathForTesting(stateDir, sessionID, workingDir string) (string, error) {
	return buildScopedSessionFilePath(stateDir, sessionID, workingDir)
}

// WriteTestSessionFile creates a scoped session file for testing.
func WriteTestSessionFile(stateDir, sessionID, workingDir string, state *ConversationState) error {
	path, err := buildScopedSessionFilePath(stateDir, sessionID, workingDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
