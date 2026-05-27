package agent

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newTrackerForShellTest builds a minimal ChangeTracker that can be
// exercised independently of a real Agent. The tracker.agent pointer
// is nil — isOutsideWorkspace handles that case by treating everything
// as in-workspace, which is exactly what we want for these tests.
// Sets shellWalkEnabled=true so the shell-walk paths run without
// requiring a configuration.Manager (production sets this via
// EnableChangeTracking → applyChangeTrackingConfig).
func newTrackerForShellTest(t *testing.T) *ChangeTracker {
	t.Helper()
	return &ChangeTracker{
		revisionID:       "test-rev",
		sessionID:        "test-session",
		enabled:          true,
		shellWalkEnabled: true,
		changes:          nil,
	}
}

func TestRecordShellMutations_RecordsDeletion(t *testing.T) {
	tracker := newTrackerForShellTest(t)
	before := map[string]*shellSnapshotEntry{
		"/work/notes.txt": {Content: []byte("important untracked notes"), Size: 25},
	}
	after := map[string]*shellSnapshotEntry{}

	tracker.RecordShellMutations(before, after, "shell_command")

	if len(tracker.changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(tracker.changes))
	}
	ch := tracker.changes[0]
	if ch.Operation != "delete" {
		t.Errorf("expected op delete, got %q", ch.Operation)
	}
	if ch.OriginalCode != "important untracked notes" {
		t.Errorf("expected original content preserved for recovery, got %q", ch.OriginalCode)
	}
	if ch.ToolCall != "shell_command" {
		t.Errorf("expected toolcall=shell_command, got %q", ch.ToolCall)
	}
}

func TestRecordShellMutations_RecordsCreation(t *testing.T) {
	tracker := newTrackerForShellTest(t)
	before := map[string]*shellSnapshotEntry{}
	after := map[string]*shellSnapshotEntry{
		"/work/new.go": {Content: []byte("package main"), Size: 12},
	}

	tracker.RecordShellMutations(before, after, "shell_command")

	if len(tracker.changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(tracker.changes))
	}
	ch := tracker.changes[0]
	if ch.Operation != "create" {
		t.Errorf("expected op create, got %q", ch.Operation)
	}
	if ch.OriginalCode != "" {
		t.Errorf("created file should have empty original, got %q", ch.OriginalCode)
	}
}

func TestRecordShellMutations_RecordsModification(t *testing.T) {
	tracker := newTrackerForShellTest(t)
	before := map[string]*shellSnapshotEntry{
		"/work/foo.go": {Content: []byte("old contents"), Size: 12},
	}
	after := map[string]*shellSnapshotEntry{
		"/work/foo.go": {Content: []byte("new contents"), Size: 12},
	}

	tracker.RecordShellMutations(before, after, "shell_command")

	if len(tracker.changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(tracker.changes))
	}
	ch := tracker.changes[0]
	if ch.Operation != "edit" {
		t.Errorf("expected op edit, got %q", ch.Operation)
	}
	if ch.OriginalCode != "old contents" || ch.NewCode != "new contents" {
		t.Errorf("expected before/after preserved, got original=%q new=%q", ch.OriginalCode, ch.NewCode)
	}
}

func TestRecordShellMutations_NoChangeIsNoOp(t *testing.T) {
	tracker := newTrackerForShellTest(t)
	before := map[string]*shellSnapshotEntry{
		"/work/foo.go": {Content: []byte("same"), Size: 4},
	}
	after := map[string]*shellSnapshotEntry{
		"/work/foo.go": {Content: []byte("same"), Size: 4},
	}

	tracker.RecordShellMutations(before, after, "shell_command")

	if len(tracker.changes) != 0 {
		t.Errorf("unchanged files should not produce manifest entries; got %d", len(tracker.changes))
	}
}

func TestRecordShellMutations_DedupesAgainstDirectHooks(t *testing.T) {
	tracker := newTrackerForShellTest(t)
	// Simulate a prior direct write_file call that already recorded the change.
	tracker.changes = append(tracker.changes, TrackedFileChange{
		FilePath:     "/work/foo.go",
		Operation:    "write",
		ToolCall:     "WriteFile",
		OriginalCode: "old via tool",
		NewCode:      "new via tool",
	})

	before := map[string]*shellSnapshotEntry{
		"/work/foo.go": {Content: []byte("old via tool"), Size: 12},
	}
	after := map[string]*shellSnapshotEntry{
		"/work/foo.go": {Content: []byte("new via tool"), Size: 12},
	}

	tracker.RecordShellMutations(before, after, "shell_command")

	if len(tracker.changes) != 1 {
		t.Errorf("shell diff should not duplicate a direct-hook entry; got %d changes", len(tracker.changes))
	}
	if tracker.changes[0].ToolCall != "WriteFile" {
		t.Errorf("dedup should keep the richer direct-hook entry; got toolcall=%q", tracker.changes[0].ToolCall)
	}
}

func TestRecordShellMutations_BinaryFlaggedAsNonRecoverable(t *testing.T) {
	tracker := newTrackerForShellTest(t)
	before := map[string]*shellSnapshotEntry{
		"/work/blob.dat": {Skipped: "binary", Size: 4096}, // content == nil → path-only entry
	}
	after := map[string]*shellSnapshotEntry{}

	tracker.RecordShellMutations(before, after, "shell_command")

	if len(tracker.changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(tracker.changes))
	}
	if !strings.HasPrefix(tracker.changes[0].OriginalCode, "[CONTENT NOT CAPTURED:") {
		t.Errorf("expected sentinel for path-only entries, got %q", tracker.changes[0].OriginalCode)
	}
}

func TestIsLikelyBinary(t *testing.T) {
	cases := []struct {
		name   string
		data   []byte
		binary bool
	}{
		{"plain text", []byte("hello world\nthis is text"), false},
		{"text with utf8", []byte("naïve résumé"), false},
		{"contains null byte", []byte("hello\x00world"), true},
		{"empty", []byte{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isLikelyBinary(c.data); got != c.binary {
				t.Errorf("isLikelyBinary(%q) = %v, want %v", c.data, got, c.binary)
			}
		})
	}
}

func TestCaptureShellSnapshot_FiltersOversizedFiles(t *testing.T) {
	dir := t.TempDir()

	// Create a small file (captured) and an oversized one (path-only).
	// Deliberately NO git init — the new walker must work in plain
	// directories.
	small := filepath.Join(dir, "small.txt")
	mustWriteFile(t, small, []byte("hi"))

	big := filepath.Join(dir, "big.bin")
	mustWriteFile(t, big, bytes.Repeat([]byte("x"), shellSnapshotMaxFileBytes+1))

	tracker := newTrackerForShellTest(t)
	snap := tracker.captureShellSnapshot(dir)

	smallEntry, ok := snap[small]
	if !ok {
		t.Fatalf("snapshot missing small file %q (snap keys: %v)", small, mapKeys(snap))
	}
	if string(smallEntry.Content) != "hi" {
		t.Errorf("small file content not captured, got %q", smallEntry.Content)
	}

	bigEntry, ok := snap[big]
	if !ok {
		t.Fatalf("snapshot missing big file %q", big)
	}
	if bigEntry.Content != nil {
		t.Errorf("big file content should NOT be captured; got %d bytes", len(bigEntry.Content))
	}
	if bigEntry.Skipped != "too large" {
		t.Errorf("expected skipped='too large', got %q", bigEntry.Skipped)
	}
}

// TestCaptureShellSnapshot_WorksWithoutGit confirms the key fix from
// the user feedback: the snapshot must work for non-git workspaces and
// for files git would call "untracked". Previously the implementation
// shelled out to `git status` and returned nil for any directory that
// wasn't a git repo, leaving shell-command mutations completely
// untracked — exactly the fragile case the user flagged.
func TestCaptureShellSnapshot_WorksWithoutGit(t *testing.T) {
	dir := t.TempDir()

	// Pure plain directory — no .git/, no init.
	mustWriteFile(t, filepath.Join(dir, "notes.txt"), []byte("user's untracked work"))
	mustWriteFile(t, filepath.Join(dir, "scratch.md"), []byte("# scratch"))

	tracker := newTrackerForShellTest(t)
	snap := tracker.captureShellSnapshot(dir)

	if len(snap) < 2 {
		t.Fatalf("expected snapshot to find both files in non-git dir, got %d entries: %v", len(snap), mapKeys(snap))
	}
	notes := snap[filepath.Join(dir, "notes.txt")]
	if notes == nil || string(notes.Content) != "user's untracked work" {
		t.Errorf("notes.txt not captured in non-git workspace: %+v", notes)
	}
}

// TestCaptureShellSnapshot_SkipsBloatDirectories confirms the walker
// prunes node_modules / .git / dist / etc. Without this, snapshotting
// a typical JS project's node_modules would blow past the
// per-snapshot budget (32 MiB) and starve coverage of real files.
func TestCaptureShellSnapshot_SkipsBloatDirectories(t *testing.T) {
	dir := t.TempDir()

	// Real source file the snapshot should capture.
	mustWriteFile(t, filepath.Join(dir, "main.go"), []byte("package main"))

	// Bloat directories we must skip. Put files inside each so a
	// regression that doesn't skip would surface their contents in
	// the snapshot map.
	for _, name := range []string{"node_modules", ".git", "dist", "__pycache__", "vendor"} {
		sub := filepath.Join(dir, name)
		if err := os.Mkdir(sub, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
		mustWriteFile(t, filepath.Join(sub, "should_not_appear.txt"), []byte("bloat"))
	}

	tracker := newTrackerForShellTest(t)
	snap := tracker.captureShellSnapshot(dir)

	if _, ok := snap[filepath.Join(dir, "main.go")]; !ok {
		t.Errorf("real source file missing from snapshot; got keys %v", mapKeys(snap))
	}
	for path := range snap {
		// Any captured path must not be under one of the skip dirs.
		rel, _ := filepath.Rel(dir, path)
		first := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]
		if shellSnapshotSkipDirs[first] {
			t.Errorf("snapshot leaked into skip dir %q via path %q", first, path)
		}
	}
}

// TestTrackShellTurn_DetectsMutationsAfterPrime is the integration
// test for the cache-based fast path: prime against a starting state,
// mutate the workspace with raw filesystem ops (mimicking what `sed`,
// `rm`, `tee`, etc. would do), then call TrackShellTurn and verify
// each mutation lands in the tracker with the correct op + recoverable
// original content.
//
// Mtime caveat: the production fast path skips re-reading files whose
// (size, mtime) match the cached entry. Some filesystems (notably
// observed on a WSL2 /tmp ext4 mount during development) return
// identical mtimes for consecutive same-file writes within a single
// kernel tick. Real shell_command invocations take milliseconds to
// run so mtime always advances, but back-to-back same-file writes in
// a unit test can land in the same tick and fool the fast path. We
// use bumpMtime to force a distinct mtime so the test is deterministic
// independent of filesystem timestamp resolution.
func TestTrackShellTurn_DetectsMutationsAfterPrime(t *testing.T) {
	dir := t.TempDir()

	original := filepath.Join(dir, "config.txt")
	mustWriteFile(t, original, []byte("port=8080"))

	doomed := filepath.Join(dir, "scratch.txt")
	mustWriteFile(t, doomed, []byte("user's untracked work"))

	tracker := newTrackerForShellTest(t)
	tracker.PrimeShellTracking(dir)

	// "shell command" mutations: modify, delete, create.
	mustWriteFile(t, original, []byte("port=9090"))
	bumpMtime(t, original)
	if err := os.Remove(doomed); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	created := filepath.Join(dir, "new.go")
	mustWriteFile(t, created, []byte("package main"))

	tracker.TrackShellTurn(dir, "shell_command")

	if len(tracker.changes) != 3 {
		t.Fatalf("expected 3 changes (1 edit + 1 delete + 1 create), got %d: %+v",
			len(tracker.changes), tracker.changes)
	}

	byPath := make(map[string]TrackedFileChange, len(tracker.changes))
	for _, ch := range tracker.changes {
		byPath[ch.FilePath] = ch
	}

	edit := byPath[original]
	if edit.Operation != "edit" {
		t.Errorf("config.txt should be op=edit, got %q", edit.Operation)
	}
	if edit.OriginalCode != "port=8080" {
		t.Errorf("config.txt original lost (expected 'port=8080'), got %q", edit.OriginalCode)
	}

	del := byPath[doomed]
	if del.Operation != "delete" {
		t.Errorf("scratch.txt should be op=delete, got %q", del.Operation)
	}
	if del.OriginalCode != "user's untracked work" {
		t.Errorf("scratch.txt original lost — this is the recovery case! Got %q", del.OriginalCode)
	}

	add := byPath[created]
	if add.Operation != "create" {
		t.Errorf("new.go should be op=create, got %q", add.Operation)
	}
}

// TestTrackShellTurn_RebasesAcrossCalls confirms the cache is updated
// after each TrackShellTurn — the second shell command's diff is
// against the state observed by the first, not the original baseline.
// Without this rebase, every subsequent shell would re-report the
// first one's mutations.
//
// See TestTrackShellTurn_DetectsMutationsAfterPrime for the mtime
// caveat; bumpMtime keeps this test deterministic.
func TestTrackShellTurn_RebasesAcrossCalls(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	mustWriteFile(t, file, []byte("v1"))

	tracker := newTrackerForShellTest(t)
	tracker.PrimeShellTracking(dir)

	// First "shell": modify the file.
	mustWriteFile(t, file, []byte("v2"))
	bumpMtime(t, file)
	tracker.TrackShellTurn(dir, "shell_command")

	if len(tracker.changes) != 1 || tracker.changes[0].NewCode != "v2" {
		t.Fatalf("first shell: expected single edit landing at v2, got %+v", tracker.changes)
	}

	// Second "shell": no further mutations. Must not re-report the
	// first edit; cache should now consider v2 the baseline.
	tracker.TrackShellTurn(dir, "shell_command")

	if len(tracker.changes) != 1 {
		t.Errorf("second shell should not re-report prior mutation; expected 1 change, got %d: %+v",
			len(tracker.changes), tracker.changes)
	}

	// Third "shell": modify again.
	mustWriteFile(t, file, []byte("v3"))
	bumpMtime(t, file)
	tracker.TrackShellTurn(dir, "shell_command")

	if len(tracker.changes) != 2 {
		t.Fatalf("third shell should land a new edit; expected 2 total changes, got %d", len(tracker.changes))
	}
	last := tracker.changes[1]
	if last.OriginalCode != "v2" || last.NewCode != "v3" {
		t.Errorf("third shell's edit should diff v2→v3 (the new baseline), got %q→%q",
			last.OriginalCode, last.NewCode)
	}
}

// TestTrackShellTurn_AutoPrimesWhenColdCalled confirms the auto-prime
// safety net: calling TrackShellTurn without a prior PrimeShellTracking
// populates the cache but doesn't fabricate "create" entries for every
// existing file (which would flood the manifest with false positives).
func TestTrackShellTurn_AutoPrimesWhenColdCalled(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "existing.go"), []byte("package main"))

	tracker := newTrackerForShellTest(t)
	// No PrimeShellTracking call — cold first invocation.
	tracker.TrackShellTurn(dir, "shell_command")

	if len(tracker.changes) != 0 {
		t.Errorf("cold first TrackShellTurn should auto-prime silently (no changes); got %d: %+v",
			len(tracker.changes), tracker.changes)
	}
	if tracker.shellCache == nil {
		t.Errorf("cold first TrackShellTurn should populate cache")
	}
}

// TestCaptureShellSnapshot_FileCountBudgetTriggers confirms the walker
// stops cleanly when the per-walk file-count budget is exceeded. This
// is the safety net for "user opened sprout in ~/" — without the
// budget, a million-file home directory would hang the agent.
func TestCaptureShellSnapshot_FileCountBudgetTriggers(t *testing.T) {
	dir := t.TempDir()

	// Create more files than the budget allows. We can't realistically
	// generate 50,000 files in a unit test, so swap the constant out
	// for a tiny one via the package-private hook.
	prevCap := overrideShellSnapshotMaxFilesForTest(5)
	defer overrideShellSnapshotMaxFilesForTest(prevCap)

	for i := 0; i < 20; i++ {
		mustWriteFile(t, filepath.Join(dir, "f"+strconv.Itoa(i)+".txt"), []byte("x"))
	}

	tracker := newTrackerForShellTest(t)
	snap := tracker.captureShellSnapshot(dir)

	if len(snap) > 5 {
		t.Errorf("snapshot honored no cap: got %d entries, expected ≤5", len(snap))
	}
	// Truncation should not have produced ANY false deletes — but the
	// captureShellSnapshot path doesn't run the diff (no `old`), so
	// this is implicit. The TrackShellTurn budget test below covers
	// the diff side.
}

// TestTrackShellTurn_TruncatedWalkSkipsFalseDeletes confirms that when
// a walk hits its budget, files in the cache that we didn't get to
// in the new walk are NOT reported as deletes (which would be a
// catastrophic false positive — every prior-walked file would look
// like the shell just deleted it).
func TestTrackShellTurn_TruncatedWalkSkipsFalseDeletes(t *testing.T) {
	dir := t.TempDir()

	for i := 0; i < 20; i++ {
		mustWriteFile(t, filepath.Join(dir, "f"+strconv.Itoa(i)+".txt"), []byte("x"))
	}

	tracker := newTrackerForShellTest(t)
	// Prime with FULL budget so all 20 files are in the cache.
	tracker.PrimeShellTracking(dir)
	if len(tracker.shellCache) != 20 {
		t.Fatalf("prime should capture all 20 files, got %d", len(tracker.shellCache))
	}

	// Now constrain the budget — next walk will only see a subset.
	prevCap := overrideShellSnapshotMaxFilesForTest(5)
	defer overrideShellSnapshotMaxFilesForTest(prevCap)

	tracker.TrackShellTurn(dir, "shell_command")

	// Critical assertion: NO deletes recorded even though most cached
	// files are "missing" from the truncated new walk.
	for _, ch := range tracker.changes {
		if ch.Operation == "delete" {
			t.Errorf("false-positive delete recorded under truncated walk: %+v", ch)
		}
	}
}

// TestWalkWorkspace_AdaptiveAutoSkipLearnsFatDirs confirms that the
// walker identifies directories with too many direct children, adds
// them to ct.autoSkipDirs, and then skips them on subsequent walks.
// This is the safety net for user-bloat dirs we didn't anticipate in
// the static skip list (e.g., a custom `releases/` directory full of
// snapshot tarballs, an misplaced data dump, a vendored mirror).
func TestWalkWorkspace_AdaptiveAutoSkipLearnsFatDirs(t *testing.T) {
	dir := t.TempDir()

	// Lower the threshold so we don't need to create 1500 files.
	prevThreshold := autoSkipFileCountThreshold
	autoSkipFileCountThreshold = 5
	defer func() { autoSkipFileCountThreshold = prevThreshold }()

	// Lean source dir — should NOT be auto-skipped.
	leanDir := filepath.Join(dir, "src")
	if err := os.Mkdir(leanDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	mustWriteFile(t, filepath.Join(leanDir, "main.go"), []byte("package main"))
	mustWriteFile(t, filepath.Join(leanDir, "util.go"), []byte("package main"))

	// Fat dir — should be auto-skipped after first walk.
	fatDir := filepath.Join(dir, "releases")
	if err := os.Mkdir(fatDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for i := 0; i < 12; i++ {
		mustWriteFile(t, filepath.Join(fatDir, "snapshot"+strconv.Itoa(i)+".bin"), []byte("x"))
	}

	tracker := newTrackerForShellTest(t)

	// First walk: the snapshot identifies the fat dir, adds it to
	// autoSkipDirs, AND immediately cleans its entries from the
	// resulting cache so the next walk's diff doesn't see them as
	// false deletes. The lean dir's files survive the cleanup.
	tracker.PrimeShellTracking(dir)
	if !tracker.autoSkipDirs[fatDir] {
		t.Fatalf("fat dir %q should have been auto-skipped after first walk; got autoSkipDirs=%v", fatDir, tracker.autoSkipDirs)
	}
	if tracker.autoSkipDirs[leanDir] {
		t.Errorf("lean dir %q should NOT be in autoSkipDirs", leanDir)
	}
	for path := range tracker.shellCache {
		rel, _ := filepath.Rel(dir, path)
		if strings.HasPrefix(rel, "releases/") {
			t.Errorf("fat-dir file %q leaked into cache after auto-skip cleanup", path)
		}
	}
	if _, ok := tracker.shellCache[filepath.Join(leanDir, "main.go")]; !ok {
		t.Errorf("lean source dir's file should remain in cache; got keys %v", mapKeys(tracker.shellCache))
	}

	// Second walk: must NOT produce false-positive deletes for the
	// fat-dir files (they were never in the cache after cleanup). And
	// the walker honors autoSkipDirs so we don't re-walk the fat dir.
	tracker.TrackShellTurn(dir, "shell_command")
	for _, ch := range tracker.changes {
		if strings.Contains(ch.FilePath, "releases/") {
			t.Errorf("false-positive change recorded for auto-skipped path: %+v", ch)
		}
	}
}

// TestCaptureShellSnapshot_SkipsSymlinks confirms the walker doesn't
// dereference symlinks. Following a symlink to /etc/passwd or
// somewhere in $HOME would leak content outside the workspace into
// the snapshot map and (worse) into the recovery buffer.
func TestCaptureShellSnapshot_SkipsSymlinks(t *testing.T) {
	dir := t.TempDir()

	target := filepath.Join(dir, "real.txt")
	mustWriteFile(t, target, []byte("real content"))

	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported in this environment: %v", err)
	}

	tracker := newTrackerForShellTest(t)
	snap := tracker.captureShellSnapshot(dir)

	if _, ok := snap[target]; !ok {
		t.Errorf("expected real file to be captured")
	}
	if _, ok := snap[link]; ok {
		t.Errorf("symlink should NOT be captured (security: could point outside workspace); got entry in snapshot")
	}
}

// --- helpers ---

func mustWriteFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

// bumpMtime forces a file's mtime to a guaranteed-distinct value so
// the (size, mtime) fast path reliably detects the change. We use a
// far-future fixed epoch + monotonic counter (not current-time-plus-1s)
// because some filesystems batch mtime updates within a tick, which
// can cause "current + 1s" on consecutive writes to collide with a
// cache's previously-bumped mtime.
//
// Production doesn't need this — real shell_command invocations take
// milliseconds to run, so mtime always advances naturally between
// PrimeShellTracking and the post-shell walk.
var bumpMtimeCounter int64

func bumpMtime(t *testing.T, path string) {
	t.Helper()
	n := atomic.AddInt64(&bumpMtimeCounter, 1)
	// Year 2033, plus N hours — comfortably distinct from any
	// real-clock mtime and monotonically increasing per call.
	next := time.Unix(2000000000, 0).Add(time.Duration(n) * time.Hour)
	if err := os.Chtimes(path, next, next); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

func mapKeys(m map[string]*shellSnapshotEntry) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

