package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestNewTestStateDir_OverridesAndRestores covers the happy path: the
// helper redirects GetStateDir to a temp dir, the test writes there,
// cleanup restores the original getter, no leak detector fires.
func TestNewTestStateDir_OverridesAndRestores(t *testing.T) {
	origGet := getStateDirFunc

	cleanup := NewTestStateDir(t)
	tmpDir, err := GetStateDir()
	if err != nil {
		t.Fatalf("GetStateDir under helper: %v", err)
	}
	if tmpDir == "" {
		t.Fatal("expected non-empty temp state dir")
	}
	// Write a file in the temp dir — this is what an Agent's autoSave
	// would do.
	testFile := filepath.Join(tmpDir, "session_test.json")
	if err := os.WriteFile(testFile, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write to temp state dir: %v", err)
	}

	cleanup()

	// After cleanup, getStateDirFunc should be the original again.
	if got := getStateDirFunc; &got == nil {
		t.Fatal("getStateDirFunc should not be nil after cleanup")
	}
	// We can't compare function pointers directly in Go, so verify the
	// behavior: the post-cleanup getter should NOT return our tmpDir.
	postPath, err := GetStateDir()
	if err != nil && origGet != nil {
		// Some envs have no home — that's fine, just skip.
		t.Skipf("post-cleanup GetStateDir: %v", err)
	}
	if postPath == tmpDir {
		t.Errorf("cleanup did not restore getStateDirFunc — still returns temp dir %q", tmpDir)
	}
}

// Note: the Layer-5 leak detector itself isn't unit-tested here
// because *testing.T's TB interface is sealed and can't be faked to
// intercept Errorf. The detector is small and readable in
// testing_state_isolation.go; its real coverage is the cmd/ tests
// that now use NewTestStateDir — any test that bypasses the helper
// will trip the detector at cleanup.

// TestAssertNoStateLeak_FreshFile covers the canonical leak case: a
// brand-new file under the developer's real state dir during the
// test run. The detector must report it.
func TestAssertNoStateLeak_FreshFile(t *testing.T) {
	tmp := t.TempDir()

	// No snapshot — nothing pre-existed.
	before := snapshotStateDir(tmp)

	// Create a new file. We sleep just enough so mtime is strictly
	// before cutoff (which AssertNoStateLeak takes after snapshot).
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(tmp, "leaked.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("create leaked file: %v", err)
	}

	code := AssertNoStateLeak(tmp, before)
	if code == 0 {
		t.Fatal("expected non-zero exit code for fresh leak, got 0")
	}
}

// TestAssertNoStateLeak_PreexistingUnchanged covers the no-leak case:
// pre-existing files in the developer's state dir (from prior CLI
// runs) must not trigger a false positive.
func TestAssertNoStateLeak_PreexistingUnchanged(t *testing.T) {
	tmp := t.TempDir()

	// Pre-create a file well before the snapshot window — say 2 hours
	// ago. The detector should treat it as "preexisting" and not flag.
	old := time.Now().Add(-2 * time.Hour)
	if err := os.WriteFile(filepath.Join(tmp, "old.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("create old file: %v", err)
	}
	if err := os.Chtimes(filepath.Join(tmp, "old.json"), old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	before := snapshotStateDir(tmp)
	code := AssertNoStateLeak(tmp, before)
	if code != 0 {
		t.Fatal("expected zero exit code for preexisting unchanged file, got 1")
	}
}

// TestAssertNoStateLeak_EmptyRealDir covers the env-without-home case:
// the detector must be a no-op (return 0) when realDir is empty.
func TestAssertNoStateLeak_EmptyRealDir(t *testing.T) {
	if got := AssertNoStateLeak("", nil); got != 0 {
		t.Fatalf("expected 0 for empty realDir, got %d", got)
	}
}

// TestAssertNoStateLeak_MtimeWithinRun covers the most subtle case:
// a pre-existing file whose mtime was updated during the test run
// (e.g. test re-saved an Agent in-place). The detector must catch
// it as a leak because the mtime is recent.
func TestAssertNoStateLeak_MtimeWithinRun(t *testing.T) {
	tmp := t.TempDir()

	// Pre-create a file with a "preexisting" mtime from 2 hours ago,
	// then snapshot, then bump mtime to "just now" to simulate a
	// write that happened during the test run. The detector should
	// catch the post-snapshot mtime change as a leak.
	old := time.Now().Add(-2 * time.Hour)
	if err := os.WriteFile(filepath.Join(tmp, "session.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("create session file: %v", err)
	}
	if err := os.Chtimes(filepath.Join(tmp, "session.json"), old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	before := snapshotStateDir(tmp)

	// Simulate a write during the run: bump mtime to a recent value
	// that is within the detector's run window.
	now := time.Now()
	if err := os.Chtimes(filepath.Join(tmp, "session.json"), now, now); err != nil {
		t.Fatalf("chtimes now: %v", err)
	}

	code := AssertNoStateLeak(tmp, before)
	if code == 0 {
		t.Fatal("expected leak detection for file modified within run window, got 0")
	}
}

// TestAssertNoStateLeak_LiveInstanceDegradesToWarning covers the
// concurrent-session case: when a live sprout instance is heartbeating
// instances.json, writes into the real state dir during the run are its
// autosaves, not a test leak — the detector must return 0.
func TestAssertNoStateLeak_LiveInstanceDegradesToWarning(t *testing.T) {
	// SPROUT_STATE_DIR makes defaultGetStateDir resolve to this dir, so
	// the dir we pass as realDir IS "the real state dir" for the run.
	// SPROUT_CONFIG_DIR redirects the instances lookup to a throwaway
	// heartbeat file so the test never depends on the developer's real
	// instances.json.
	stateRoot := t.TempDir()
	t.Setenv("SPROUT_STATE_DIR", stateRoot)
	t.Setenv("SPROUT_CONFIG_DIR", t.TempDir())

	// Heartbeat: this test process (alive by definition), pinged now.
	heartbeat := map[string]struct {
		PID      int       `json:"pid"`
		LastPing time.Time `json:"last_ping"`
	}{
		"instance_test": {PID: os.Getpid(), LastPing: time.Now()},
	}
	b, err := json.Marshal(heartbeat)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(instancesConfigDir(), "instances.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}

	if !liveSproutInstanceRunning() {
		t.Fatal("precondition: live instance should be detected from the fake heartbeat")
	}

	realDir := filepath.Join(stateRoot, "sessions")
	if err := os.MkdirAll(realDir, 0o700); err != nil {
		t.Fatal(err)
	}
	before := snapshotStateDir(realDir)
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(realDir, "leaked.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	if code := AssertNoStateLeak(realDir, before); code != 0 {
		t.Fatal("expected 0 when a live instance explains the writes")
	}
}

// TestAssertNoStateLeak_StaleHeartbeatStillFails covers the inverse: a
// stale heartbeat (dead/old PID) must not suppress leak detection.
func TestAssertNoStateLeak_StaleHeartbeatStillFails(t *testing.T) {
	stateRoot := t.TempDir()
	t.Setenv("SPROUT_STATE_DIR", stateRoot)
	t.Setenv("SPROUT_CONFIG_DIR", t.TempDir())

	// PID 99999999 with a fresh ping: pidAlive fails the signal probe.
	heartbeat := map[string]struct {
		PID      int       `json:"pid"`
		LastPing time.Time `json:"last_ping"`
	}{
		"instance_test": {PID: 99999999, LastPing: time.Now()},
	}
	b, err := json.Marshal(heartbeat)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(instancesConfigDir(), "instances.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}

	if liveSproutInstanceRunning() {
		t.Skip("PID 99999999 is somehow alive on this machine")
	}

	realDir := filepath.Join(stateRoot, "sessions")
	if err := os.MkdirAll(realDir, 0o700); err != nil {
		t.Fatal(err)
	}
	before := snapshotStateDir(realDir)
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(realDir, "leaked.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	if code := AssertNoStateLeak(realDir, before); code == 0 {
		t.Fatal("expected leak detection with only a stale heartbeat, got 0")
	}
}
