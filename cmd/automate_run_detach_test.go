//go:build !js

package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// TestOpenDetachLogFile verifies the detach-mode log wiring: the directory
// is created under <sproutDir>/automate/logs/, the file is writable, and
// the returned path points at it. This is the mechanism that decouples a
// detached workflow child's stdio from the launcher's lifetime — the
// SIGPIPE-death fix for `sprout automate run --detach`.
func TestOpenDetachLogFile(t *testing.T) {
	sproutDir := filepath.Join(t.TempDir(), ".sprout")
	sessionID := "cli-automate-deadbeefdeadbeef"

	f, path, err := openDetachLogFile(sproutDir, sessionID)
	if err != nil {
		t.Fatalf("openDetachLogFile: %v", err)
	}
	defer f.Close()

	wantDir := filepath.Join(sproutDir, "automate", "logs")
	if fi, err := os.Stat(wantDir); err != nil || !fi.IsDir() {
		t.Fatalf("log dir not created at %s: %v", wantDir, err)
	}
	wantPath := filepath.Join(wantDir, sessionID+".log")
	if path != wantPath {
		t.Fatalf("path = %q, want %q", path, wantPath)
	}
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("log file not created: %v", err)
	}
	if _, err := f.WriteString("probe\n"); err != nil {
		t.Fatalf("log file not writable: %v", err)
	}

	// Second call with the same session truncates rather than appending —
	// matches os.O_TRUNC semantics and keeps restarts from stacking logs.
	f2, _, err := openDetachLogFile(sproutDir, sessionID)
	if err != nil {
		t.Fatalf("second openDetachLogFile: %v", err)
	}
	defer f2.Close()
	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(data) != 0 {
		t.Fatalf("expected truncate-on-reopen, got %d bytes", len(data))
	}
}

// TestAutomateDetachFlagDefaults pins the flag default so the attached
// streaming behavior stays the default and --detach is opt-in.
func TestAutomateDetachFlagDefaults(t *testing.T) {
	if automateDetach {
		t.Fatal("automateDetach should default to false")
	}
	flag := automateCmd.PersistentFlags().Lookup("detach")
	if flag == nil {
		t.Fatal("--detach flag not registered on automate command group")
	}
	if flag.DefValue != "false" {
		t.Fatalf("--detach default = %q, want \"false\"", flag.DefValue)
	}
}
