package cmd

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// TestTerminalSubscriber_VerbosityLiveRead is a regression test for the
// bug where output_verbosity was read once at startup and cached in the
// subscriber state as a plain string. Changing verbosity via /settings
// mid-session had no effect until the user restarted sprout.
//
// The fix: the subscriber stores the *configuration.Manager and reads
// cfg.OutputVerbosity live on every event via isCompact(). This test
// flips the config between "default" and "compact" and verifies the
// subscriber reflects the change without reconstruction.
func TestTerminalSubscriber_VerbosityLiveRead(t *testing.T) {
	configDir := t.TempDir() + "/.sprout"
	mgr, err := configuration.NewManagerWithDir(configDir)
	if err != nil {
		t.Fatalf("NewManagerWithDir: %v", err)
	}

	// Start in default mode — not compact.
	state := newTerminalSubscriberState(mgr)
	if state.isCompact() {
		t.Fatal("isCompact() = true with default config; want false")
	}

	// Flip to compact mid-session (simulates /settings output_verbosity compact).
	if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.OutputVerbosity = configuration.OutputVerbosityCompact
		return nil
	}); err != nil {
		t.Fatalf("UpdateConfigNoSave: %v", err)
	}

	// The SAME subscriber state should now report compact — no restart,
	// no reconstruction needed.
	if !state.isCompact() {
		t.Fatal("isCompact() = false after setting compact; the subscriber did not pick up the live config change")
	}

	// Flip back to default.
	if err := mgr.UpdateConfigNoSave(func(cfg *configuration.Config) error {
		cfg.OutputVerbosity = configuration.OutputVerbosityDefault
		return nil
	}); err != nil {
		t.Fatalf("UpdateConfigNoSave: %v", err)
	}
	if state.isCompact() {
		t.Fatal("isCompact() = true after reverting to default")
	}
}

// TestTerminalSubscriber_NilConfigManagerIsNonCompact verifies the nil
// fallback: non-agent callers (tests, nil chatAgent) pass a nil config
// manager. isCompact() must return false (non-compact) rather than
// panicking, so the subscriber renders normally.
func TestTerminalSubscriber_NilConfigManagerIsNonCompact(t *testing.T) {
	state := newTerminalSubscriberState(nil)
	if state.isCompact() {
		t.Fatal("isCompact() = true with nil config manager; want false")
	}
}
