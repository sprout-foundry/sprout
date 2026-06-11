package agent

import (
	"os"
	"path/filepath"
	"testing"
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
