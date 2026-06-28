//go:build !js

package webui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/search"
)

// TestMain isolates session-state persistence for the webui package's
// tests. The sync_recovery_test.go suite (and others) builds real
// Agents to exercise the websocket bridge — without this hook, those
// Agents' autoSaveState() leaks session JSONs into the developer's
// real ~/.sprout/sessions/. See cmd/main_test.go for the broader story.
//
// pkg/agent/persistence.go's init() captures defaultGetStateDir() at
// package-init time to seed the global search index updater — that
// happens before TestMain runs and would otherwise point at the real
// $HOME/sessions/search-index.json. We re-aim it at the temp dir here
// and restore the previous updater after the test run so any leak
// the snapshotter detects points at the real failure, not at the
// indexer's stale init-time path.
//
// The !js build tag matches the rest of the package: webui tests
// don't run under WASM, so the TestMain only fires for the native
// build where the leak actually happens.
func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "sprout-webui-test-state-*")
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

	// Re-aim the search index updater at the temp sessions dir so the
	// SP-083-3 SaveSession hook doesn't leak search-index.json into the
	// developer's real $HOME. Stops the previous updater (and cancels
	// any pending timer) before swapping; restores it after the run.
	prevUpdater := search.ResetGlobalUpdaterForTest()
	defer func() {
		if prevUpdater != nil {
			search.RestoreGlobalUpdater(prevUpdater)
		}
	}()
	indexPath := filepath.Join(sessionsDir, "search-index.json")
	search.InitGlobalUpdater(indexPath, sessionsDir)

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
