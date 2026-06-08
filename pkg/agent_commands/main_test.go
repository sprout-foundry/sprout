package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// TestMain isolates session-state persistence for the agent_commands
// package's tests. See the matching block in cmd/main_test.go for the
// rationale (leaky autoSaveState writes a session JSON to the
// developer's real ~/.sprout/sessions/ on every Agent constructed in a
// test). Without this hook, tests like TestRunPersonaSmoke that build
// real Agents through slash-command handlers leak state files.
func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "sprout-agent-commands-test-state-*")
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
