package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// TestMain isolates session-state persistence for the commands
// package's tests. See cmd/main_test.go for the rationale.
func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "sprout-commands-test-state-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: create temp state dir: %v\n", err)
		os.Exit(1)
	}
	sessionsDir := filepath.Join(tmpDir, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "TestMain: mkdir sessions: %v\n", err)
		_ = os.RemoveAll(tmpDir)
		os.Exit(1)
	}

	realDir, beforeSnapshot := agent.SnapshotRealStateDir()

	restore := agent.SetTestStateDirHook(sessionsDir)
	code := m.Run()
	restore()

	leakCode := agent.AssertNoStateLeak(realDir, beforeSnapshot)
	_ = os.RemoveAll(tmpDir)

	if code == 0 {
		code = leakCode
	}
	os.Exit(code)
}
