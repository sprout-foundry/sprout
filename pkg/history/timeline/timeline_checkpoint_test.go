package timeline

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/history"
)

// seedCheckpointStore redirects pkg/history's change and checkpoint stores to
// temp dirs for the test and returns a workdir for files the recorder writes
// (so no test file lands in the package directory).
func seedCheckpointStore(t *testing.T) string {
	t.Helper()
	prevC, prevR := history.GetPathsForTesting()
	prevCp := history.GetCheckpointsDir()
	tmp := t.TempDir()
	workdir := t.TempDir()
	history.SetPathsForTesting(filepath.Join(tmp, "changes"), filepath.Join(tmp, "revisions"))
	history.SetCheckpointsDirForTesting(filepath.Join(tmp, "checkpoints"))
	t.Cleanup(func() {
		history.SetPathsForTesting(prevC, prevR)
		history.SetCheckpointsDirForTesting(prevCp)
	})
	return workdir
}

// recordRevision records one revision with a single file change written under
// workdir, so a restore has a real file to revert.
func recordRevision(t *testing.T, workdir, revID, name, original, updated string) {
	t.Helper()
	if _, err := history.RecordBaseRevision(revID, "prompt", "response", nil); err != nil {
		t.Fatalf("RecordBaseRevision: %v", err)
	}
	path := filepath.Join(workdir, name)
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := history.RecordChangeWithDetails(revID, path, original, updated, "edit", "", "", "", "m"); err != nil {
		t.Fatalf("RecordChangeWithDetails: %v", err)
	}
}

// TestSummarizeCheckpoint_Templates pins the deterministic checkpoint summary
// templates for every origin.
func TestSummarizeCheckpoint_Templates(t *testing.T) {
	tests := []struct {
		name string
		cp   history.Checkpoint
		want string
	}{
		{
			name: "verification with files",
			cp:   history.Checkpoint{Origin: history.CheckpointVerification, RevisionID: "rev-1", Files: []string{"a.go", "b.go"}},
			want: "checkpoint (verification): changed 2 files @ rev-1",
		},
		{
			name: "deploy no files",
			cp:   history.Checkpoint{Origin: history.CheckpointDeploy},
			want: "checkpoint (deploy): no files",
		},
		{
			name: "manual single file",
			cp:   history.Checkpoint{Origin: history.CheckpointManual, RevisionID: "rev-9", Files: []string{"a.go"}},
			want: "checkpoint (manual): changed 1 file @ rev-9",
		},
		{
			name: "restore",
			cp:   history.Checkpoint{Origin: history.CheckpointRestore, RevisionID: "rev-2", Files: []string{"a.go"}},
			want: "restored checkpoint: changed 1 file @ rev-2",
		},
		{
			name: "unknown origin surfaced",
			cp:   history.Checkpoint{Origin: history.CheckpointOrigin("weird")},
			want: "checkpoint (weird): no files",
		},
		{
			name: "empty origin",
			cp:   history.Checkpoint{},
			want: "checkpoint: no files",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SummarizeCheckpoint(tt.cp); got != tt.want {
				t.Errorf("summary = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestBuild_CheckpointEntry pins the third entry kind: a checkpoint renders
// its summary and carries the history checkpoint value.
func TestBuild_CheckpointEntry(t *testing.T) {
	ts := time.Date(2026, 3, 3, 9, 0, 0, 0, time.UTC)
	cp := history.Checkpoint{
		ID:         "cp-1",
		Origin:     history.CheckpointVerification,
		Timestamp:  ts,
		RevisionID: "rev-1",
		Files:      []string{"a.go"},
	}

	entries := Build([]EntryInput{{Checkpoint: &cp}}, OldestFirst)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	e := entries[0]
	if e.Kind() != KindCheckpoint {
		t.Fatalf("kind = %q, want %q", e.Kind(), KindCheckpoint)
	}
	if e.Checkpoint.Checkpoint.ID != "cp-1" {
		t.Errorf("checkpoint id = %q, want cp-1", e.Checkpoint.Checkpoint.ID)
	}
	if got, want := e.Summary(), "checkpoint (verification): changed 1 file @ rev-1"; got != want {
		t.Errorf("summary = %q, want %q", got, want)
	}
}

// TestBuild_KindRankTieBreak pins the deterministic ordering at an equal
// timestamp: a change set sorts before a checkpoint, which sorts before a
// deploy.
func TestBuild_KindRankTieBreak(t *testing.T) {
	ts := time.Date(2026, 3, 3, 9, 0, 0, 0, time.UTC)
	rev := &history.RevisionGroup{
		RevisionID: "rev-a",
		Timestamp:  ts,
		Changes:    []history.ChangeLog{{Filename: "f.go", Status: activeStatus, OriginalCode: "x\n", NewCode: "y\n"}},
	}
	cp := &history.Checkpoint{ID: "cp-a", Timestamp: ts, Origin: history.CheckpointManual}

	entries := Build([]EntryInput{
		{Checkpoint: cp},
		{Revision: rev},
	}, OldestFirst)
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].Kind() != KindChangeSet {
		t.Errorf("first kind = %q, want change_set (rank 0)", entries[0].Kind())
	}
	if entries[1].Kind() != KindCheckpoint {
		t.Errorf("second kind = %q, want checkpoint (rank 1)", entries[1].Kind())
	}
}

// TestBuildTimeline_IncludesCheckpoints is the acceptance test for the
// shared model: BuildTimeline reads the checkpoint store, so an automatically
// captured verification/deploy checkpoint and a restore (itself a checkpoint)
// all appear on the timeline.
func TestBuildTimeline_IncludesCheckpoints(t *testing.T) {
	workdir := seedCheckpointStore(t)
	recordRevision(t, workdir, "rev-1", "a.go", "package a\n", "package a\n// x\n")

	verCP, err := history.CreateCheckpointInWorkspace(workdir, history.CheckpointVerification, "verification passed", nil)
	if err != nil {
		t.Fatalf("CreateCheckpoint: %v", err)
	}
	depCP, err := history.CreateCheckpointInWorkspace(workdir, history.CheckpointDeploy, "deployed", nil)
	if err != nil {
		t.Fatalf("CreateCheckpoint: %v", err)
	}

	entries, err := BuildTimelineInWorkspace(workdir, nil, nil, OldestFirst)
	if err != nil {
		t.Fatalf("BuildTimeline: %v", err)
	}

	// one change set + two checkpoints
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3 (1 change set + 2 checkpoints)", len(entries))
	}
	if entries[0].Kind() != KindChangeSet {
		t.Errorf("entry 0 kind = %q, want change_set", entries[0].Kind())
	}
	seen := map[string]bool{}
	for _, e := range entries {
		if e.Kind() == KindCheckpoint {
			seen[e.Checkpoint.Checkpoint.ID] = true
		}
	}
	if !seen[verCP.ID] || !seen[depCP.ID] {
		t.Errorf("timeline is missing checkpoints: saw %v, want %s and %s", seen, verCP.ID, depCP.ID)
	}
}

// TestBuildTimeline_ShowsRestoreAsEntry asserts a restore appears on the
// timeline as its own entry — nothing is lost.
func TestBuildTimeline_ShowsRestoreAsEntry(t *testing.T) {
	workdir := seedCheckpointStore(t)
	recordRevision(t, workdir, "rev-1", "a.go", "package a\n", "package a\n// x\n")

	cp, err := history.CreateCheckpointInWorkspace(workdir, history.CheckpointManual, "point", nil)
	if err != nil {
		t.Fatalf("CreateCheckpoint: %v", err)
	}

	before, err := BuildTimelineInWorkspace(workdir, nil, nil, OldestFirst)
	if err != nil {
		t.Fatalf("BuildTimeline: %v", err)
	}

	if _, err := history.RestoreCheckpointInWorkspace(workdir, cp.ID); err != nil {
		t.Fatalf("RestoreCheckpoint: %v", err)
	}

	after, err := BuildTimelineInWorkspace(workdir, nil, nil, OldestFirst)
	if err != nil {
		t.Fatalf("BuildTimeline: %v", err)
	}
	if len(after) != len(before)+1 {
		t.Fatalf("entries after restore = %d, want %d (restore adds one entry)", len(after), len(before)+1)
	}

	var restores int
	for _, e := range after {
		if e.Kind() == KindCheckpoint && e.Checkpoint.Checkpoint.Origin == history.CheckpointRestore {
			restores++
		}
	}
	if restores != 1 {
		t.Errorf("timeline shows %d restore entries, want 1", restores)
	}
}

// TestBuild_CheckpointSummaryMissingFilesIsStable guards the nil/empty file
// list path (a checkpoint with no captured revision).
func TestBuild_CheckpointSummaryMissingFilesIsStable(t *testing.T) {
	cp := history.Checkpoint{ID: "cp-x", Origin: history.CheckpointManual}
	entries := Build([]EntryInput{{Checkpoint: &cp}}, OldestFirst)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if got, want := entries[0].Summary(), "checkpoint (manual): no files"; got != want {
		t.Errorf("summary = %q, want %q", got, want)
	}
}
