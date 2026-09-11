package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/history"
)

// newRecoveryTestAgent builds a minimal agent with an enabled, empty
// ChangeTracker rooted at ws — the shape handleRecoverFile needs.
func newRecoveryTestAgent(t *testing.T, ws string) *Agent {
	t.Helper()
	a := &Agent{workspaceRoot: ws, state: NewAgentStateManager(false)}
	a.EnableChangeTracking("recovery test")
	if a.GetChangeTracker() == nil {
		t.Fatal("tracker not enabled")
	}
	return a
}

// setupPersistedRecoveryTest isolates config and history I/O, then
// redirects history paths AFTER agent construction (the redirect must
// be the last write to the package-level paths). Returns the agent and
// the isolated workspace root.
func setupPersistedRecoveryTest(t *testing.T) (*Agent, string) {
	t.Helper()
	_, configCleanup := configuration.NewTestManager(t)
	t.Cleanup(configCleanup)

	ws := t.TempDir()
	a := newRecoveryTestAgent(t, ws)

	// Redirect now — after EnableChangeTracking fired
	// InitializeHistoryPaths — so RecordChangeWithDetails and
	// FindPersistedOriginal read/write the same isolated store.
	setHistory := isolateHistoryForTest(t)
	setHistory()

	return a, ws
}

// Cross-session recovery: when the session buffer has no record for a
// path but the persisted history store does, recover_file restores the
// stored original instead of dead-ending with "no tracked change".
func TestHandleRecoverFile_FallsBackToPersistedStore(t *testing.T) {
	a, ws := setupPersistedRecoveryTest(t)

	target := filepath.Join(ws, "persisted.go")

	// Record a change directly into the history store (as a prior
	// session's Commit would have).
	if err := history.RecordChangeWithDetails(
		"rev-persisted-test", target,
		"package persisted\n", "package changed\n",
		"edit via EditFile", "test note",
		"instructions", "response", "test-model",
	); err != nil {
		t.Fatalf("RecordChangeWithDetails: %v", err)
	}

	// Disk currently holds the post-change content (the agent's last
	// write), so the staleness guard passes.
	if err := os.WriteFile(target, []byte("package changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := handleRecoverFile(context.Background(), a, map[string]interface{}{"path": target})
	if err != nil {
		t.Fatalf("handleRecoverFile: %v", err)
	}

	var res struct {
		Recovered bool   `json:"recovered"`
		Message   string `json:"message"`
	}
	if jsonErr := json.Unmarshal([]byte(out), &res); jsonErr != nil {
		t.Fatalf("not JSON: %v\n%s", jsonErr, out)
	}
	if !res.Recovered {
		t.Fatalf("expected persisted-store recovery to succeed, got: %s", out)
	}

	got, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "package persisted\n" {
		t.Errorf("restored content: want stored original, got %q", string(got))
	}
}

// When neither the session buffer nor the store has the path, the
// failure message must say so explicitly.
func TestHandleRecoverFile_PersistedMissReportsBoth(t *testing.T) {
	a, ws := setupPersistedRecoveryTest(t)

	target := filepath.Join(ws, "never-tracked.go")
	if err := os.WriteFile(target, []byte("content\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := handleRecoverFile(context.Background(), a, map[string]interface{}{"path": target})
	if err != nil {
		t.Fatalf("handleRecoverFile: %v", err)
	}

	if !strings.Contains(out, "session buffer") || !strings.Contains(out, "persisted history") {
		t.Errorf("miss message should mention both sources, got: %s", out)
	}
}
