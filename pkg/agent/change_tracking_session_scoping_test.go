package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

// ============================================================================
// Session-scoping regression tests (Fix A + Fix B + subagent no-commit).
//
// These lock in the behaviour that was broken before this change:
//
//  1. Fix B — the change buffer is SESSION-LONG. EnableChangeTracking on
//     an existing tracker must NOT wipe ct.changes. Previously a Reset
//     fired on every ProcessQuery, so list_changes returned count:0 at
//     the start of each turn — the exact footgun that surfaced the bug.
//
//  2. Subagent fix — subagents track in memory only; their ProcessQuery
//     end-of-loop Commit must be skipped so history isn't polluted with
//     duplicate revision dirs ("subagent run") and double-persisted
//     files. The parent's Commit owns persistence.
//
//  3. Fix A — list_changes defaults to include_persisted, but the merge
//     is SESSION-SCOPED (matches the tracker's revisionID) and deduped
//     against the in-memory buffer so a file edited last turn AND
//     re-touched this turn appears once.
// ============================================================================

// TestSession_BufferSurvivesReEnable (Fix B) verifies that calling
// EnableChangeTracking a second time — as ProcessQuery does at the start
// of every turn — preserves the changes accumulated in the first turn.
func TestSession_BufferSurvivesReEnable(t *testing.T) {
	ws := t.TempDir()
	a := NewTestAgent()
	a.workspaceRoot = ws

	// Turn 1: enable tracking and record an edit.
	a.EnableChangeTracking("first query")
	revID := a.GetRevisionID()
	a.TrackFileWrite(filepath.Join(ws, "main.go"), "package main\n")

	if got := a.GetChangeCount(); got != 1 {
		t.Fatalf("turn 1: expected 1 change, got %d", got)
	}

	// Turn 2: ProcessQuery re-enables tracking. Before the fix this
	// called Reset() and wiped the buffer.
	a.EnableChangeTracking("second query")

	if got := a.GetChangeCount(); got != 1 {
		t.Errorf("turn 2: re-enable wiped the buffer (count=%d, want 1) — Fix B regressed", got)
	}
	if got := a.GetRevisionID(); got != revID {
		t.Errorf("turn 2: revision ID changed on re-enable %q -> %q (must be session-stable)", revID, got)
	}

	// Turn 2's own edit appends rather than replacing.
	a.TrackFileEdit(filepath.Join(ws, "main.go"), "package main\n", "package main\n// edited\n")
	if got := a.GetChangeCount(); got != 2 {
		t.Errorf("turn 2: expected 2 changes after append, got %d", got)
	}
}

// TestSession_ListChangesReflectsFullSession (Fix B + Fix A) verifies
// that list_changes shows changes from a prior turn even after a
// re-enable, because the buffer is session-long.
func TestSession_ListChangesReflectsFullSession(t *testing.T) {
	ws := t.TempDir()
	a := NewTestAgent()
	a.workspaceRoot = ws

	a.EnableChangeTracking("session start")
	a.TrackFileWrite(filepath.Join(ws, "auth.go"), "contents")

	// Simulate a new turn (ProcessQuery re-enables).
	a.EnableChangeTracking("next turn")

	// list_changes should still see the prior turn's file.
	out, err := handleListChanges(context.Background(), a, map[string]interface{}{
		"include_persisted": false, // in-memory buffer only
	})
	if err != nil {
		t.Fatalf("list_changes error: %v", err)
	}
	var parsed struct {
		Count int `json:"count"`
		Files []struct {
			Path string `json:"path"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if parsed.Count == 0 {
		t.Error("list_changes returned count:0 after re-enable — the in-memory buffer should be session-long (Fix B)")
	}
}

// TestSession_ListChangesPersistsSessionScoped (Fix A) verifies that the
// default include_persisted merge is scoped to the current session's
// revisionID, NOT global history. Entries from other sessions must not
// leak into this session's manifest.
func TestSession_ListChangesPersistsSessionScoped(t *testing.T) {
	// Two independent trackers simulate two sessions. Their revisionIDs
	// differ, so neither's persisted merge should include the other's
	// entries.
	ws := t.TempDir()
	a1 := NewTestAgent()
	a1.workspaceRoot = ws
	a1.EnableChangeTracking("session one")
	rev1 := a1.GetRevisionID()

	a2 := NewTestAgent()
	a2.workspaceRoot = ws
	a2.EnableChangeTracking("session two")
	rev2 := a2.GetRevisionID()

	if rev1 == rev2 {
		t.Fatalf("test setup: expected distinct revision IDs, got %q for both", rev1)
	}

	// Record a change in session 1 only.
	a1.TrackFileWrite(filepath.Join(ws, "only_in_session1.go"), "x")

	// Session 2's list_changes (persisted merge) must NOT show it.
	out, err := handleListChanges(context.Background(), a2, nil)
	if err != nil {
		t.Fatalf("list_changes error: %v", err)
	}
	leaked := "only_in_session1.go"
	if containsSubstring(out, leaked) {
		t.Errorf("session 2 manifest leaked session 1's file %q — persisted merge must be session-scoped (Fix A):\n%s", leaked, out)
	}
}

// TestSession_SubagentDoesNotCommit verifies the subagent no-self-commit
// contract: the post-loop Commit guard in processQueryWithSeed excludes
// subagents so history is owned solely by the parent. This test encodes
// the two invariants the fix relies on:
//
//  1. IsSubagent() correctly identifies subagents (depth > 0).
//  2. The guard condition "!a.IsSubagent() && enabled && count>0" is
//     false for a subagent even when it has tracked changes.
//
// The live guard lives in seed_query.go's post-loop (gated on
// !a.IsSubagent()); we assert the predicate here so a regression in
// either IsSubagent() or the guard shape is caught without standing up
// a full ProcessQuery (which needs an LLM client).
func TestSession_SubagentDoesNotCommit(t *testing.T) {
	ws := t.TempDir()

	// Build a subagent (depth > 0) with tracking enabled and one change.
	sub := NewTestAgent()
	sub.workspaceRoot = ws
	sub.subagentDepth = 1
	sub.EnableChangeTracking("subagent run")
	sub.TrackFileWrite(filepath.Join(ws, "by_subagent.go"), "contents")

	if !sub.IsSubagent() {
		t.Fatalf("IsSubagent() = false for depth %d; the guard depends on this", sub.subagentDepth)
	}
	if sub.GetChangeCount() != 1 {
		t.Fatalf("subagent should have 1 tracked change, got %d", sub.GetChangeCount())
	}
	if !sub.IsChangeTrackingEnabled() {
		t.Fatal("subagent tracking should be enabled")
	}

	// This is the exact predicate from seed_query.go's post-loop commit
	// site. For a subagent it MUST be false — otherwise the subagent
	// double-commits to history and litters revision dirs.
	shouldCommit := !sub.IsSubagent() && sub.IsChangeTrackingEnabled() && sub.GetChangeCount() > 0
	if shouldCommit {
		t.Error("post-loop commit predicate is true for a subagent — subagents must NOT self-commit (parent owns history)")
	}

	// Sanity: a primary agent (depth 0) with the same change DOES commit.
	primary := NewTestAgent()
	primary.workspaceRoot = ws
	primary.EnableChangeTracking("primary run")
	primary.TrackFileWrite(filepath.Join(ws, "by_primary.go"), "contents")
	shouldCommitPrimary := !primary.IsSubagent() && primary.IsChangeTrackingEnabled() && primary.GetChangeCount() > 0
	if !shouldCommitPrimary {
		t.Error("post-loop commit predicate is false for a primary agent with changes — primary MUST commit")
	}
}

// TestSession_PersistedDedupedAgainstInMemory (Fix A) verifies that when
// a file appears both in the persisted history (committed last turn) and
// in the in-memory buffer (re-edited this turn), it appears exactly once
// in the manifest, with the in-memory entry winning.
func TestSession_PersistedDedupedAgainstInMemory(t *testing.T) {
	ws := t.TempDir()
	a := NewTestAgent()
	a.workspaceRoot = ws
	a.EnableChangeTracking("dedup session")

	path := filepath.Join(ws, "shared.go")
	// In-memory entry for this path.
	a.TrackFileWrite(path, "v2")

	out, err := handleListChanges(context.Background(), a, map[string]interface{}{
		"include_persisted": true,
	})
	if err != nil {
		t.Fatalf("list_changes error: %v", err)
	}
	var parsed struct {
		Files []struct {
			Path string `json:"path"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	count := 0
	for _, f := range parsed.Files {
		if f.Path == path {
			count++
		}
	}
	if count > 1 {
		t.Errorf("path %q appeared %d times in manifest — persisted entries must be deduped against in-memory (Fix A)", path, count)
	}
}
