package agent

import (
	"path/filepath"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// ChangeTracker.MergeChild — SP-059 Phase 2c
//
// These tests verify that a subagent's tracked changes are merged into
// the parent tracker and tagged with Source so list_changes /
// recover_file / revert_my_changes can attribute them correctly.
// ---------------------------------------------------------------------------

func TestMergeChild_BasicMerge(t *testing.T) {
	agent := NewTestAgent()
	ct := NewChangeTracker(agent, "primary instruction")

	changes := []TrackedFileChange{
		{
			FilePath:     "/ws/created.go",
			OriginalCode: "",
			NewCode:      "package main\n",
			Operation:    "create",
			ToolCall:     "WriteFile",
			Timestamp:    time.Now(),
		},
		{
			FilePath:     "/ws/edited.go",
			OriginalCode: "old\n",
			NewCode:      "new\n",
			Operation:    "edit",
			ToolCall:     "EditFile",
			Timestamp:    time.Now(),
		},
		{
			FilePath:     "/ws/written.go",
			OriginalCode: "old\n",
			NewCode:      "newer\n",
			Operation:    "write",
			ToolCall:     "WriteFile",
			Timestamp:    time.Now(),
		},
	}

	ct.MergeChild(changes, "subagent:coder")

	got := ct.GetChanges()
	if len(got) != len(changes) {
		t.Fatalf("expected %d changes, got %d", len(changes), len(got))
	}

	for i, want := range changes {
		g := got[i]
		if g.Source != "subagent:coder" {
			t.Errorf("change[%d] Source = %q, want %q", i, g.Source, "subagent:coder")
		}
		// Content fields must be preserved so recover_file works.
		if g.FilePath != want.FilePath {
			t.Errorf("change[%d] FilePath = %q, want %q", i, g.FilePath, want.FilePath)
		}
		if g.OriginalCode != want.OriginalCode {
			t.Errorf("change[%d] OriginalCode = %q, want %q", i, g.OriginalCode, want.OriginalCode)
		}
		if g.NewCode != want.NewCode {
			t.Errorf("change[%d] NewCode = %q, want %q", i, g.NewCode, want.NewCode)
		}
		if g.Operation != want.Operation {
			t.Errorf("change[%d] Operation = %q, want %q", i, g.Operation, want.Operation)
		}
	}
}

func TestMergeChild_NoOpWhenDisabled(t *testing.T) {
	agent := NewTestAgent()
	ct := NewChangeTracker(agent, "primary instruction")
	ct.Disable()

	changes := []TrackedFileChange{
		{FilePath: "/ws/a.go", NewCode: "x\n", Operation: "create", Timestamp: time.Now()},
	}
	ct.MergeChild(changes, "subagent:coder")

	if got := ct.GetChangeCount(); got != 0 {
		t.Fatalf("expected 0 changes when disabled, got %d", got)
	}
}

func TestMergeChild_EmptyAndNilSafe(t *testing.T) {
	agent := NewTestAgent()
	ct := NewChangeTracker(agent, "primary instruction")

	// Must not panic on nil.
	ct.MergeChild(nil, "subagent:coder")
	if got := ct.GetChangeCount(); got != 0 {
		t.Fatalf("nil input: expected 0 changes, got %d", got)
	}

	// Must not panic on empty slice.
	ct.MergeChild([]TrackedFileChange{}, "subagent:coder")
	if got := ct.GetChangeCount(); got != 0 {
		t.Fatalf("empty input: expected 0 changes, got %d", got)
	}
}

func TestMergeChild_DoesNotMutateInput(t *testing.T) {
	agent := NewTestAgent()
	ct := NewChangeTracker(agent, "primary instruction")

	changes := []TrackedFileChange{
		{FilePath: "/ws/a.go", NewCode: "a\n", Operation: "create", Timestamp: time.Now()},
		{FilePath: "/ws/b.go", NewCode: "b\n", Operation: "create", Timestamp: time.Now()},
	}

	ct.MergeChild(changes, "subagent:coder")

	// The input slice's entries should still have empty Source —
	// MergeChild must copy rather than mutate in place.
	for i, ch := range changes {
		if ch.Source != "" {
			t.Errorf("input[%d].Source = %q, want empty (input must not be mutated)", i, ch.Source)
		}
	}
}

func TestMergeChild_TagsSource(t *testing.T) {
	ws := t.TempDir()
	agent := NewTestAgent()
	agent.workspaceRoot = ws
	ct := NewChangeTracker(agent, "primary instruction")

	// Record a pre-existing primary-agent edit (no Source).
	primaryFile := filepath.Join(ws, "primary.go")
	if err := ct.TrackFileWrite(primaryFile, "primary content\n"); err != nil {
		t.Fatalf("TrackFileWrite: %v", err)
	}

	// Now merge in subagent changes.
	subagentChanges := []TrackedFileChange{
		{FilePath: filepath.Join(ws, "sub.go"), NewCode: "sub\n", Operation: "create", Timestamp: time.Now()},
	}
	ct.MergeChild(subagentChanges, "subagent:coder")

	got := ct.GetChanges()
	if len(got) != 2 {
		t.Fatalf("expected 2 changes, got %d", len(got))
	}

	// The primary edit (first entry) should keep empty Source.
	if got[0].Source != "" {
		t.Errorf("primary change Source = %q, want empty", got[0].Source)
	}
	if got[0].FilePath != primaryFile {
		t.Errorf("primary change FilePath = %q, want %q", got[0].FilePath, primaryFile)
	}

	// The merged subagent edit should be tagged.
	if got[1].Source != "subagent:coder" {
		t.Errorf("merged change Source = %q, want %q", got[1].Source, "subagent:coder")
	}
}

func TestAgent_MergeSubagentChanges(t *testing.T) {
	ws := t.TempDir()
	agent := NewTestAgent()
	agent.workspaceRoot = ws
	agent.changeTracker = NewChangeTracker(agent, "primary instruction")

	changes := []TrackedFileChange{
		{
			FilePath:     filepath.Join(ws, "sub_created.go"),
			OriginalCode: "",
			NewCode:      "package sub\n",
			Operation:    "create",
			ToolCall:     "WriteFile",
			Timestamp:    time.Now(),
		},
		{
			FilePath:     filepath.Join(ws, "sub_edited.go"),
			OriginalCode: "before\n",
			NewCode:      "after\n",
			Operation:    "edit",
			ToolCall:     "EditFile",
			Timestamp:    time.Now(),
		},
	}

	agent.MergeSubagentChanges(changes, "coder")

	// The merged files should appear in the agent's tracked file list.
	trackedFiles := agent.GetTrackedFiles()
	if len(trackedFiles) != 2 {
		t.Fatalf("expected 2 tracked files, got %d (%v)", len(trackedFiles), trackedFiles)
	}

	// Each should carry the subagent:coder source tag.
	got := agent.GetChangeTracker().GetChanges()
	for i, ch := range got {
		if ch.Source != "subagent:coder" {
			t.Errorf("change[%d] Source = %q, want %q", i, ch.Source, "subagent:coder")
		}
	}

	// GetChangesSummary should reflect the merged entries (non-empty).
	summary := agent.GetChangesSummary()
	if summary == "" || summary == "Change tracking is not enabled" {
		t.Errorf("summary should reflect merged changes, got %q", summary)
	}
}

func TestAgent_MergeSubagentChanges_EmptyPersonaUsesBareTag(t *testing.T) {
	ws := t.TempDir()
	agent := NewTestAgent()
	agent.workspaceRoot = ws
	agent.changeTracker = NewChangeTracker(agent, "primary instruction")

	changes := []TrackedFileChange{
		{FilePath: filepath.Join(ws, "sub.go"), NewCode: "x\n", Operation: "create", Timestamp: time.Now()},
	}
	// Empty persona → bare "subagent" source.
	agent.MergeSubagentChanges(changes, "")

	got := agent.GetChangeTracker().GetChanges()
	if len(got) != 1 {
		t.Fatalf("expected 1 change, got %d", len(got))
	}
	if got[0].Source != "subagent" {
		t.Errorf("Source = %q, want %q", got[0].Source, "subagent")
	}
}

func TestAgent_MergeSubagentChanges_NoOpWhenDisabled(t *testing.T) {
	agent := NewTestAgent()
	agent.changeTracker = NewChangeTracker(agent, "primary instruction")
	agent.changeTracker.Disable()

	changes := []TrackedFileChange{
		{FilePath: "/ws/sub.go", NewCode: "x\n", Operation: "create", Timestamp: time.Now()},
	}
	agent.MergeSubagentChanges(changes, "coder")

	if got := agent.GetChangeCount(); got != 0 {
		t.Fatalf("expected 0 changes when tracking disabled, got %d", got)
	}
}

func TestAgent_MergeSubagentChanges_NilTrackerSafe(t *testing.T) {
	agent := NewTestAgent()
	// changeTracker is nil; must not panic.
	agent.MergeSubagentChanges([]TrackedFileChange{
		{FilePath: "/ws/sub.go", NewCode: "x\n", Operation: "create", Timestamp: time.Now()},
	}, "coder")
}
