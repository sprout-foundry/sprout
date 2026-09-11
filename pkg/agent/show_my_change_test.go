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

// The diff endpoint's envelope must match the frontend's
// ChangeDiffResponse ({found, path, op, diff}). The historical
// implementation routed through handleListChanges and returned the
// list_changes manifest instead, so the panel's diff modal always
// rendered "(no tracked change)".
func TestShowMyChange_ReturnsDiffEnvelope(t *testing.T) {
	_, configCleanup := configuration.NewTestManager(t)
	defer configCleanup()

	ws := t.TempDir()
	a := newRecoveryTestAgent(t, ws)

	path := filepath.Join(ws, "demo.go")
	if err := os.WriteFile(path, []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.TrackFileWrite(path, "v1\n", "v2\n"); err != nil {
		t.Fatal(err)
	}

	out, err := a.ShowMyChange(path)
	if err != nil {
		t.Fatalf("ShowMyChange: %v", err)
	}

	var res struct {
		Found bool   `json:"found"`
		Path  string `json:"path"`
		Diff  string `json:"diff"`
	}
	if jsonErr := json.Unmarshal([]byte(out), &res); jsonErr != nil {
		t.Fatalf("not a diff envelope: %v\n%s", jsonErr, out)
	}
	if !res.Found {
		t.Fatalf("expected found=true, got: %s", out)
	}
	if !strings.Contains(res.Diff, "v1") || !strings.Contains(res.Diff, "v2") {
		t.Errorf("diff should show before/after content, got: %q", res.Diff)
	}
}

// A path only present in the persisted store still renders a diff.
func TestShowMyChange_PersistedFallback(t *testing.T) {
	_, configCleanup := configuration.NewTestManager(t)
	t.Cleanup(configCleanup)

	ws := t.TempDir()
	a := newRecoveryTestAgent(t, ws)
	setHistory := isolateHistoryForTest(t)
	setHistory()

	path := filepath.Join(ws, "old.go")
	if err := history.RecordChangeWithDetails(
		"rev-diff-test", path,
		"old content\n", "new content\n",
		"edit via EditFile", "note",
		"instructions", "response", "test-model",
	); err != nil {
		t.Fatal(err)
	}

	out, err := a.ShowMyChange(path)
	if err != nil {
		t.Fatalf("ShowMyChange: %v", err)
	}

	var res struct {
		Found bool   `json:"found"`
		Diff  string `json:"diff"`
	}
	if jsonErr := json.Unmarshal([]byte(out), &res); jsonErr != nil {
		t.Fatalf("not a diff envelope: %v\n%s", jsonErr, out)
	}
	if !res.Found || !strings.Contains(res.Diff, "old content") {
		t.Fatalf("expected persisted diff, got: %s", out)
	}
}

// Untouched paths report found=false rather than an empty manifest.
func TestShowMyChange_UnknownPath(t *testing.T) {
	_, configCleanup := configuration.NewTestManager(t)
	defer configCleanup()

	ws := t.TempDir()
	a := newRecoveryTestAgent(t, ws)

	out, err := a.ShowMyChange(filepath.Join(ws, "missing.go"))
	if err != nil {
		t.Fatalf("ShowMyChange: %v", err)
	}
	if !strings.Contains(out, `"found": false`) {
		t.Fatalf("expected found=false, got: %s", out)
	}
}

// handleRevertMyChanges on a disabled tracker must report
// enabled=false so callers can tell "could not serve" from "served,
// nothing matched".
func TestRevertMyChanges_DisabledTrackerReportsEnabledFalse(t *testing.T) {
	a := &Agent{state: NewAgentStateManager(false)}
	// No EnableChangeTracking call — tracker nil.

	out, err := handleRevertMyChanges(context.Background(), a, map[string]interface{}{"scope": "all"})
	if err != nil {
		t.Fatalf("handleRevertMyChanges: %v", err)
	}

	var res struct {
		Enabled bool `json:"enabled"`
	}
	if jsonErr := json.Unmarshal([]byte(out), &res); jsonErr != nil {
		t.Fatalf("not JSON: %v", jsonErr)
	}
	if res.Enabled {
		t.Fatalf("expected enabled=false for disabled tracker, got: %s", out)
	}
}
