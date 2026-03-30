package configuration

import (
	"path/filepath"
	"testing"
)

// NewTestManager creates a configuration Manager backed by an isolated temp
// directory so that tests never read, modify, or create files in the caller's
// real ~/.ledit config.  It returns the manager and a cleanup func that the
// caller should defer.
//
// Usage:
//
//	mgr, cleanup := NewTestManager(t)
//	defer cleanup()
func NewTestManager(t *testing.T) (*Manager, func()) {
	t.Helper()

	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, ".ledit")

	mgr, err := NewManagerWithDir(configDir)
	if err != nil {
		t.Fatalf("NewManagerWithDir(%q) failed: %v", configDir, err)
	}

	// Keep LEDIT_CONFIG pointing at the temp dir for the test's lifetime so
	// that any subsequent Save()/UpdateConfig calls remain isolated.
	t.Setenv("LEDIT_CONFIG", configDir)

	cleanup := func() {}

	return mgr, cleanup
}
