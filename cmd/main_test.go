package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// TestMain isolates session-state persistence for every test in this
// package. Without this, tests that build real Agents
// (cmd.runSeamlessPlanning, cmd.runAgentQuery, etc.) inherit the
// default state dir from agent.GetStateDir() — which is the
// developer's actual ~/.sprout/sessions/ on the host.
//
// We measured the leak before adding this: ~90 mock-provider session
// JSONs (9.2 MB) had accumulated in the developer's home over weeks
// of `go test ./cmd/...` runs, polluting their real session corpus
// with synthetic "Test response from mock provider" data. The leak
// detector that ships alongside this isolation block (see
// pkg/agent/testing_state_isolation.go::NewTestStateDir) catches any
// future test that bypasses the override.
//
// Approach: create one shared temp dir for the whole package run,
// install it via agent.SetTestStateDirHook, run the suite, restore
// the original hook + remove the temp dir on exit. A shared temp
// directory across tests is intentional — the per-Agent session_id
// is timestamp-based, so collisions don't happen and the shared dir
// keeps the test setup cheap.
func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "sprout-cmd-test-state-*")
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

	// Layer-5 detector: snapshot the developer's REAL state dir
	// before installing the override. If a test bypasses the hook
	// and writes to the real dir anyway, AssertNoStateLeak prints a
	// noisy stderr warning AND returns a non-zero code that we OR
	// into the test exit so CI fails.
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
