package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/search"
)

// isolateStateAndIndexForTest redirects both pkg/agent's session state
// directory AND pkg/search's global index updater into the same t.TempDir(),
// so SaveStateScoped → search.MarkSessionDirty writes go to a temp dir
// instead of the developer's real ~/.sprout/sessions/.
//
// Why not just NewTestStateDir(t)? That helper calls t.TempDir() internally
// and doesn't expose the path; to point search.GlobalUpdater at the SAME
// state dir we need to derive it ourselves. SetTestStateDirHook (the
// lower-level primitive NewTestStateDir wraps) plus search.ResetGlobalUpdaterForTest
// give us both isolations with shared path control. The trade-off is no
// built-in Layer-5 leak detector on the real state dir — but since every
// known write path (session JSONs via SaveStateScoped, and search-index.json
// via MarkSessionDirty → rebuildAndSave) is redirected here, there is
// nothing left to leak into ~/.sprout/sessions/.
//
// Returns the sessionsDir (also suitable as the workingDir arg to
// LoadStateScoped / SaveStateScoped) and registers a t.Cleanup that
// restores both globals.
func isolateStateAndIndexForTest(t *testing.T) (sessionsDir string) {
	t.Helper()

	base := t.TempDir()
	sessionsDir = filepath.Join(base, ".sprout", "sessions")
	if err := os.MkdirAll(sessionsDir, 0o700); err != nil {
		t.Fatalf("isolateStateAndIndexForTest: mkdir sessions: %v", err)
	}
	stateRestore := SetTestStateDirHook(sessionsDir)

	// search.GlobalUpdater was wired at package init in persistence.go
	// against the developer's real ~/.sprout/sessions/. Without this
	// reset, the debounced rebuild triggered by MarkSessionDirty would
	// write search-index.json into the real state dir and trip the
	// pkg/agent_commands TestMain leak detector.
	oldUpdater := search.ResetGlobalUpdaterForTest()
	indexPath := filepath.Join(sessionsDir, "search-index.json")
	search.GlobalUpdater = search.NewIndexUpdater(indexPath, sessionsDir)

	t.Cleanup(func() {
		search.RestoreGlobalUpdater(oldUpdater)
		stateRestore()
	})
	return sessionsDir
}

// TestRotateSessionClearsAndAssignsNewID verifies the happy-path invariants of
// RotateSession: in-memory state is cleared, and the agent is assigned a new
// SessionID that differs from the prior one.
func TestRotateSessionClearsAndAssignsNewID(t *testing.T) {
	isolateStateAndIndexForTest(t)

	a := newTestAgent(t)

	// Pre-seed a session ID + a single message so we can prove they are wiped.
	priorID := "session_prior_id_for_test"
	a.SetSessionID(priorID)
	a.state.AddMessage(api.Message{Role: "user", Content: "hello"})
	a.state.SetPreviousSummary("some prior summary")

	newID, err := a.RotateSession()
	if err != nil {
		t.Fatalf("RotateSession returned error: %v", err)
	}

	if newID == "" {
		t.Fatal("RotateSession returned empty new ID")
	}
	if newID == priorID {
		t.Fatalf("RotateSession returned the prior ID %q; expected a fresh one", priorID)
	}
	if !strings.HasPrefix(newID, "session_") {
		t.Errorf("new session ID %q does not start with %q", newID, "session_")
	}

	if got := a.GetSessionID(); got != newID {
		t.Errorf("GetSessionID() = %q, want %q", got, newID)
	}
	if msgs := a.GetMessages(); len(msgs) != 0 {
		t.Errorf("after RotateSession expected 0 messages, got %d", len(msgs))
	}
	if got := a.GetPreviousSummary(); got != "" {
		t.Errorf("after RotateSession expected empty summary, got %q", got)
	}
}

// TestRotateSessionPersistsPriorSnapshot verifies the load-bearing invariant:
// the prior session must remain loadable from disk after rotation.
func TestRotateSessionPersistsPriorSnapshot(t *testing.T) {
	sessionsDir := isolateStateAndIndexForTest(t)

	a := newTestAgent(t)

	priorID := "session_prior_persist_test"
	a.SetSessionID(priorID)
	a.state.AddMessage(api.Message{Role: "user", Content: "this message must survive rotation"})

	newID, err := a.RotateSession()
	if err != nil {
		t.Fatalf("RotateSession returned error: %v", err)
	}
	if newID == priorID {
		t.Fatalf("RotateSession returned the prior ID %q; expected a fresh one", priorID)
	}

	// The prior session file must still be loadable via the same scoped API the
	// CLI uses (LoadStateScoped). The pre-rotation messages must round-trip.
	// We pass sessionsDir as the workingDir arg so that the scope hash matches
	// what SaveStateScoped derived during the rotate; the lookup also has a
	// name-based fallback for any scope mismatch, but matching directly is
	// cleaner and matches what the production CLI does.
	loaded, err := a.LoadStateScoped(priorID, sessionsDir)
	if err != nil {
		t.Fatalf("LoadStateScoped(prior ID) failed: %v", err)
	}

	found := false
	for _, m := range loaded.Messages {
		if m.Role == "user" && strings.Contains(m.Content, "this message must survive rotation") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("prior session file did not contain the pre-rotation message; got %d messages", len(loaded.Messages))
	}

	// And the new session must NOT have been written under the prior ID.
	if loaded.SessionID == newID {
		t.Errorf("prior session file is stamped with the new ID %q; expected the prior ID", newID)
	}
}

// TestRotateSessionAbortsOnSnapshotFailure verifies that if SaveStateScoped
// fails for the prior session, RotateSession returns the error WITHOUT
// clearing in-memory state and WITHOUT assigning a new session ID.
func TestRotateSessionAbortsOnSnapshotFailure(t *testing.T) {
	isolateStateAndIndexForTest(t)

	a := newTestAgent(t)

	// normalizeSessionID rejects IDs whose post-prefix tail contains a path
	// separator (see pkg/agent/persistence.go). Force that failure by setting
	// a session ID whose cleaned form includes a slash.
	badID := "session_bogus/id"
	a.SetSessionID(badID)
	a.state.AddMessage(api.Message{Role: "user", Content: "must survive a failed rotate"})

	_, err := a.RotateSession()
	if err == nil {
		t.Fatal("RotateSession returned nil error; expected a snapshot failure")
	}

	// In-memory state must be untouched: same session ID, same messages.
	if got := a.GetSessionID(); got != badID {
		t.Errorf("after failed RotateSession, session ID = %q; want unchanged %q", got, badID)
	}
	if msgs := a.GetMessages(); len(msgs) != 1 || msgs[0].Content != "must survive a failed rotate" {
		t.Errorf("after failed RotateSession, messages changed unexpectedly: %+v", msgs)
	}
}

// TestRotateSessionFromEmptyID verifies that RotateSession works on a freshly
// created agent that has no prior session ID. The snapshot step must be
// skipped, then the agent must still be assigned a fresh session ID.
func TestRotateSessionFromEmptyID(t *testing.T) {
	isolateStateAndIndexForTest(t)

	a := newTestAgent(t)
	if got := a.GetSessionID(); got != "" {
		t.Fatalf("fresh test agent should have empty session ID, got %q", got)
	}

	newID, err := a.RotateSession()
	if err != nil {
		t.Fatalf("RotateSession returned error on empty prior ID: %v", err)
	}
	if newID == "" {
		t.Fatal("RotateSession returned empty new ID for fresh agent")
	}
	if !strings.HasPrefix(newID, "session_") {
		t.Errorf("new session ID %q does not start with %q", newID, "session_")
	}
	if got := a.GetSessionID(); got != newID {
		t.Errorf("GetSessionID() = %q, want %q", got, newID)
	}
}
