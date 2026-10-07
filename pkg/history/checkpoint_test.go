package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// seedCheckpointHistory redirects the change and checkpoint stores to a temp
// dir for the test and returns a recorder that records one revision with the
// given file changes (written to disk so a restore can revert them). The
// returned paths map gives the absolute path for each file name.
func seedCheckpointHistory(t *testing.T) (func(revID string, files map[string][2]string), string) {
	t.Helper()
	prevC, prevR := GetPathsForTesting()
	prevCp := GetCheckpointsDir()
	tmp := t.TempDir()
	workdir := t.TempDir()
	SetPathsForTesting(filepath.Join(tmp, "changes"), filepath.Join(tmp, "revisions"))
	SetCheckpointsDirForTesting(filepath.Join(tmp, "checkpoints"))
	t.Cleanup(func() {
		SetPathsForTesting(prevC, prevR)
		SetCheckpointsDirForTesting(prevCp)
	})

	record := func(revID string, files map[string][2]string) {
		t.Helper()
		if _, err := RecordBaseRevision(revID, "prompt "+revID, "response "+revID, nil); err != nil {
			t.Fatalf("RecordBaseRevision(%q): %v", revID, err)
		}
		for name, content := range files {
			path := filepath.Join(workdir, name)
			original, updated := content[0], content[1]
			if err := os.WriteFile(path, []byte(updated), 0644); err != nil {
				t.Fatalf("write %q: %v", path, err)
			}
			if err := RecordChangeWithDetails(revID, path, original, updated, "edit "+name, "", "", "", "test-model"); err != nil {
				t.Fatalf("RecordChangeWithDetails(%q, %q): %v", revID, path, err)
			}
		}
	}
	return record, workdir
}

func TestNewCheckpoint_CapturesRevision(t *testing.T) {
	group := &RevisionGroup{
		RevisionID: "rev-1",
		Changes: []ChangeLog{
			{Filename: "b.go", Status: activeStatus},
			{Filename: "a.go", Status: activeStatus},
			{Filename: "a.go", Status: activeStatus},
		},
	}
	cp := NewCheckpoint(CheckpointVerification, group, []string{"scope-1"}, "passed")
	if cp.RevisionID != "rev-1" {
		t.Errorf("revision = %q, want rev-1", cp.RevisionID)
	}
	if got := cp.Files; len(got) != 2 || got[0] != "a.go" || got[1] != "b.go" {
		t.Errorf("files = %v, want [a.go b.go] (deduped, sorted)", got)
	}
	if cp.Origin != CheckpointVerification {
		t.Errorf("origin = %q, want verification", cp.Origin)
	}
	if len(cp.ScopeIDs) != 1 || cp.ScopeIDs[0] != "scope-1" {
		t.Errorf("scope IDs = %v, want [scope-1]", cp.ScopeIDs)
	}
	if cp.ID == "" {
		t.Error("checkpoint has no ID")
	}
	if cp.Timestamp.IsZero() {
		t.Error("checkpoint has zero timestamp")
	}
}

func TestNewCheckpoint_NilCaptured(t *testing.T) {
	cp := NewCheckpoint(CheckpointManual, nil, nil, "no state")
	if cp.RevisionID != "" {
		t.Errorf("revision = %q, want empty for a nil capture", cp.RevisionID)
	}
	if len(cp.Files) != 0 {
		t.Errorf("files = %v, want empty", cp.Files)
	}
	if cp.ID == "" {
		t.Error("checkpoint has no ID")
	}
}

// TestCreateCheckpoint_RecordsCurrentRevision pins the automatic/on-demand
// seam: CreateCheckpoint captures the most recent revision of the store.
func TestCreateCheckpoint_RecordsCurrentRevision(t *testing.T) {
	record, _ := seedCheckpointHistory(t)
	record("rev-old", map[string][2]string{"old.go": {"x\n", "x\ny\n"}})
	record("rev-new", map[string][2]string{"new.go": {"p\n", "p\nq\n"}})

	cp, err := CreateCheckpoint(CheckpointManual, "manual", nil)
	if err != nil {
		t.Fatalf("CreateCheckpoint: %v", err)
	}
	if cp.RevisionID != "rev-new" {
		t.Errorf("captured revision = %q, want rev-new (most recent)", cp.RevisionID)
	}

	// The checkpoint is persisted.
	stored, found, err := GetCheckpoint(cp.ID)
	if err != nil {
		t.Fatalf("GetCheckpoint: %v", err)
	}
	if !found {
		t.Fatalf("checkpoint %q not found after create", cp.ID)
	}
	if stored.RevisionID != "rev-new" {
		t.Errorf("stored revision = %q, want rev-new", stored.RevisionID)
	}
}

func TestCreateCheckpoint_EmptyHistoryStillRecords(t *testing.T) {
	seedCheckpointHistory(t)
	cp, err := CreateCheckpoint(CheckpointManual, "empty", nil)
	if err != nil {
		t.Fatalf("CreateCheckpoint: %v", err)
	}
	if cp.RevisionID != "" {
		t.Errorf("revision = %q, want empty for empty history", cp.RevisionID)
	}
}

// TestListCheckpoints_Order pins the oldest-first / newest-first orders and
// that a missing store reads as an empty list.
func TestListCheckpoints_Order(t *testing.T) {
	seedCheckpointHistory(t)

	// Empty store is not an error.
	empty, err := ListCheckpoints(false)
	if err != nil {
		t.Fatalf("ListCheckpoints on empty store: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("empty store returned %d checkpoints, want 0", len(empty))
	}

	a, err := SaveCheckpoint(Checkpoint{ID: "cp-a", Origin: CheckpointManual, Summary: "a", Timestamp: mustTime(t, "2026-01-01T00:00:00Z")})
	if err != nil {
		t.Fatalf("SaveCheckpoint a: %v", err)
	}
	b, err := SaveCheckpoint(Checkpoint{ID: "cp-b", Origin: CheckpointDeploy, Summary: "b", Timestamp: mustTime(t, "2026-01-01T01:00:00Z")})
	if err != nil {
		t.Fatalf("SaveCheckpoint b: %v", err)
	}

	oldest, err := ListCheckpoints(false)
	if err != nil {
		t.Fatalf("ListCheckpoints oldest: %v", err)
	}
	if len(oldest) != 2 || oldest[0].ID != a.ID || oldest[1].ID != b.ID {
		t.Errorf("oldest-first = %v, want [%s %s]", ids(oldest), a.ID, b.ID)
	}

	newest, err := ListCheckpoints(true)
	if err != nil {
		t.Fatalf("ListCheckpoints newest: %v", err)
	}
	if len(newest) != 2 || newest[0].ID != b.ID || newest[1].ID != a.ID {
		t.Errorf("newest-first = %v, want [%s %s]", ids(newest), b.ID, a.ID)
	}
}

// TestRestoreCheckpoint_RevertsAndRecordsItself is the core acceptance test:
// restoring is one action, it reverts the captured revision's files, and the
// restore is itself recorded (as a checkpoint) so the timeline shows it.
func TestRestoreCheckpoint_RevertsAndRecordsItself(t *testing.T) {
	record, workdir := seedCheckpointHistory(t)
	target := filepath.Join(workdir, "a.go")
	record("rev-1", map[string][2]string{"a.go": {"package a\n", "package a\n// changed\n"}})

	// Capture a checkpoint at rev-1, then restore it.
	cp, err := CreateCheckpoint(CheckpointManual, "before", nil)
	if err != nil {
		t.Fatalf("CreateCheckpoint: %v", err)
	}
	if cp.RevisionID != "rev-1" {
		t.Fatalf("captured revision = %q, want rev-1", cp.RevisionID)
	}

	// The file on disk currently holds the changed content.
	if got, _ := os.ReadFile(target); string(got) != "package a\n// changed\n" {
		t.Fatalf("precondition: a.go = %q", got)
	}

	restore, err := RestoreCheckpoint(cp.ID)
	if err != nil {
		t.Fatalf("RestoreCheckpoint: %v", err)
	}

	// The revert wrote the original content back.
	if got, _ := os.ReadFile(target); string(got) != "package a\n" {
		t.Errorf("after restore a.go = %q, want the original content", got)
	}

	// The restore is itself a recorded checkpoint (a timeline entry).
	if restore.Origin != CheckpointRestore {
		t.Errorf("restore origin = %q, want restore", restore.Origin)
	}
	if restore.RevisionID != "rev-1" {
		t.Errorf("restore revision = %q, want rev-1", restore.RevisionID)
	}

	all, err := ListCheckpoints(false)
	if err != nil {
		t.Fatalf("ListCheckpoints: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("got %d checkpoints after restore, want 2 (original + restore)", len(all))
	}
	if all[1].Origin != CheckpointRestore {
		t.Errorf("last checkpoint origin = %q, want restore", all[1].Origin)
	}
}

func TestRestoreCheckpoint_UnknownIsError(t *testing.T) {
	seedCheckpointHistory(t)
	if _, err := RestoreCheckpoint("nope"); err == nil {
		t.Fatal("RestoreCheckpoint with unknown id: want error, got nil")
	}
}

func TestRestoreCheckpoint_NoRevisionIsError(t *testing.T) {
	seedCheckpointHistory(t)
	cp, err := SaveCheckpoint(Checkpoint{ID: "cp-empty", Origin: CheckpointManual})
	if err != nil {
		t.Fatalf("SaveCheckpoint: %v", err)
	}
	if _, err := RestoreCheckpoint(cp.ID); err == nil {
		t.Fatal("RestoreCheckpoint of a revision-less checkpoint: want error, got nil")
	}
}

func TestSafeCheckpointFilename(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"cp-1", "cp-1"},
		{"../../etc/passwd", "____etc_passwd"},
		{"a/b\\c", "a_b_c"},
		{"", "checkpoint"},
		{"  spaced  ", "spaced"},
	}
	for _, tt := range tests {
		if got := SafeCheckpointFilename(tt.in); got != tt.want {
			t.Errorf("SafeCheckpointFilename(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSortedIdsTieBreak(t *testing.T) {
	ts := mustTime(t, "2026-01-01T00:00:00Z")
	cps := []Checkpoint{
		{ID: "cp-b", Timestamp: ts},
		{ID: "cp-a", Timestamp: ts},
	}
	sortCheckpoints(cps, false)
	if cps[0].ID != "cp-a" || cps[1].ID != "cp-b" {
		t.Errorf("oldest-first tie-break = %v, want [cp-a cp-b]", ids(cps))
	}
	sortCheckpoints(cps, true)
	if cps[0].ID != "cp-b" || cps[1].ID != "cp-a" {
		t.Errorf("newest-first tie-break = %v, want [cp-b cp-a]", ids(cps))
	}
}

func ids(cps []Checkpoint) []string {
	out := make([]string, 0, len(cps))
	for _, cp := range cps {
		out = append(out, cp.ID)
	}
	return out
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatalf("parse time %q: %v", s, err)
	}
	return parsed
}

// TestCheckpointWorkspace_ReaderMatchesWriter is the regression test for the
// store-scoping contract: a checkpoint written workspace-scoped
// (CreateCheckpointInWorkspace, the store the automatic seams use) is found
// by the workspace-scoped reader/lister, and is NOT found in the process-wide
// store. Without this, an automatic checkpoint would be written somewhere the
// timeline never reads.
func TestCheckpointWorkspace_ReaderMatchesWriter(t *testing.T) {
	seedCheckpointHistory(t)
	workspace := t.TempDir()

	cp, err := CreateCheckpointInWorkspace(workspace, CheckpointVerification, "passed", nil)
	if err != nil {
		t.Fatalf("CreateCheckpointInWorkspace: %v", err)
	}

	inWorkspace, err := ListCheckpointsInWorkspace(workspace, false)
	if err != nil {
		t.Fatalf("ListCheckpointsInWorkspace: %v", err)
	}
	if len(inWorkspace) != 1 || inWorkspace[0].ID != cp.ID {
		t.Fatalf("workspace store = %v, want the created checkpoint %s", ids(inWorkspace), cp.ID)
	}

	if _, found, err := GetCheckpointInWorkspace(workspace, cp.ID); err != nil || !found {
		t.Fatalf("GetCheckpointInWorkspace(%s) found=%v err=%v, want found", cp.ID, found, err)
	}

	// The process-wide store must not contain it (proves the scoping is real,
	// not an accident of both stores pointing at one directory).
	if _, found, err := GetCheckpoint(cp.ID); err != nil {
		t.Fatalf("GetCheckpoint: %v", err)
	} else if found {
		t.Errorf("process-wide store found a workspace-scoped checkpoint %s", cp.ID)
	}
}

// TestRestoreCheckpointInWorkspace_RecordsRestoreInSameStore pins that the
// restore record lands back in the same workspace store the checkpoint was
// read from.
func TestRestoreCheckpointInWorkspace_RecordsRestoreInSameStore(t *testing.T) {
	record, workdir := seedCheckpointHistory(t)
	target := filepath.Join(workdir, "a.go")
	record("rev-1", map[string][2]string{"a.go": {"package a\n", "package a\n// changed\n"}})

	cp, err := CreateCheckpointInWorkspace(workdir, CheckpointManual, "point", nil)
	if err != nil {
		t.Fatalf("CreateCheckpointInWorkspace: %v", err)
	}
	if _, err := RestoreCheckpointInWorkspace(workdir, cp.ID); err != nil {
		t.Fatalf("RestoreCheckpointInWorkspace: %v", err)
	}

	got, _ := os.ReadFile(target)
	if string(got) != "package a\n" {
		t.Errorf("restored content = %q, want the original", got)
	}

	cps, err := ListCheckpointsInWorkspace(workdir, false)
	if err != nil {
		t.Fatalf("ListCheckpointsInWorkspace: %v", err)
	}
	if len(cps) != 2 || cps[1].Origin != CheckpointRestore {
		t.Fatalf("workspace store after restore = %v, want [%s, restore]", ids(cps), cp.ID)
	}
}

// TestCreateCheckpointForRevision_UsesGivenRevision pins that the
// revision-explicit capture names exactly the revision it was given, even
// when another revision is the store's most recent. The automatic seams
// depend on this: a checkpoint that pointed at the store head rather than
// the revision the turn produced could restore the wrong state.
func TestCreateCheckpointForRevision_UsesGivenRevision(t *testing.T) {
	record, _ := seedCheckpointHistory(t)
	// An older revision the caller names, and a newer one that is the head.
	record("rev-old", map[string][2]string{"old.go": {"a\n", "a\nb\n"}})
	record("rev-new", map[string][2]string{"new.go": {"c\n", "c\nd\n"}})
	workspace := t.TempDir()

	cp, err := CreateCheckpointForRevision(workspace, CheckpointVerification, "rev-old", "passed", nil)
	if err != nil {
		t.Fatalf("CreateCheckpointForRevision: %v", err)
	}
	if cp.RevisionID != "rev-old" {
		t.Errorf("revision = %q, want rev-old", cp.RevisionID)
	}
	// The revision summary enriches from the store, so the older revision's
	// file is the one recorded.
	if len(cp.Files) != 1 || filepath.Base(cp.Files[0]) != "old.go" {
		t.Errorf("files = %v, want the rev-old file old.go", cp.Files)
	}

	stored, found, err := GetCheckpointInWorkspace(workspace, cp.ID)
	if err != nil || !found {
		t.Fatalf("GetCheckpointInWorkspace(%s) found=%v err=%v", cp.ID, found, err)
	}
	if stored.RevisionID != "rev-old" {
		t.Errorf("stored revision = %q, want rev-old", stored.RevisionID)
	}
}

// TestListCheckpoints_DoesNotCreateStore pins that reading a checkpoint
// store never creates it: a read of a workspace that has no checkpoints is
// not an mkdir side effect under a caller-supplied path.
func TestListCheckpoints_DoesNotCreateStore(t *testing.T) {
	prevCp := GetCheckpointsDir()
	dir := filepath.Join(t.TempDir(), "does", "not", "exist")
	SetCheckpointsDirForTesting(dir)
	t.Cleanup(func() { SetCheckpointsDirForTesting(prevCp) })

	cps, err := ListCheckpoints(false)
	if err != nil {
		t.Fatalf("ListCheckpoints: %v", err)
	}
	if len(cps) != 0 {
		t.Fatalf("got %d checkpoints from a missing store, want 0", len(cps))
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("read created the checkpoint store %s (stat err = %v); a read must not mkdir", dir, err)
	}
}
