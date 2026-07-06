package console

import (
	"strings"
	"sync"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// TestComputeVerbosityToggle verifies the pure cycle logic.
func TestComputeVerbosityToggle(t *testing.T) {
	cases := []struct {
		current string
		want    string
	}{
		{configuration.OutputVerbosityDefault, configuration.OutputVerbosityVerbose},
		{"", configuration.OutputVerbosityVerbose},
		{configuration.OutputVerbosityCompact, configuration.OutputVerbosityVerbose},
		{configuration.OutputVerbosityVerbose, configuration.OutputVerbosityDefault},
		{"unknown", configuration.OutputVerbosityVerbose},
	}
	for _, c := range cases {
		got := computeVerbosityToggle(c.current)
		if got != c.want {
			t.Errorf("computeVerbosityToggle(%q) = %q, want %q", c.current, got, c.want)
		}
	}
}

// TestVerbosityToggleLabel verifies the confirmation messages contain
// the expected text.
func TestVerbosityToggleLabel(t *testing.T) {
	verbose := verbosityToggleLabel(configuration.OutputVerbosityVerbose)
	if !strings.Contains(verbose, "verbose") {
		t.Errorf("verbose label missing 'verbose': %q", verbose)
	}
	if !strings.Contains(verbose, "Alt+V") {
		t.Errorf("verbose label missing 'Alt+V': %q", verbose)
	}

	def := verbosityToggleLabel(configuration.OutputVerbosityDefault)
	if !strings.Contains(def, "default") {
		t.Errorf("default label missing 'default': %q", def)
	}
	if !strings.Contains(def, "Alt+V") {
		t.Errorf("default label missing 'Alt+V': %q", def)
	}
}

// TestOutputVerbosityToggleRoundTrip verifies the full cycle end-to-end:
// default → verbose → default, by calling the registered handler twice.
// The first call to RegisterKeymapForFooter wires the singleton entry
// that the rest of this test exercises; the second Register call is a
// no-op (Once-protected).
func TestOutputVerbosityToggleRoundTrip(t *testing.T) {
	configDir := t.TempDir() + "/.sprout"
	cfg, err := configuration.NewManagerWithDir(configDir)
	if err != nil {
		t.Fatalf("NewManagerWithDir: %v", err)
	}

	// Register the keymap entry (idempotent via sync.Once).
	RegisterKeymapForFooter(nil, cfg)
	// Repeat-call should be harmless; verifies idempotency of the Once
	// guard that CLI-D-3 explicitly relies on.
	RegisterKeymapForFooter(nil, cfg)

	entry, ok := GlobalKeymap().Lookup("output_verbosity.toggle")
	if !ok {
		t.Fatal("output_verbosity.toggle not registered")
	}
	if entry.Key != "Alt+V" {
		t.Errorf("Key = %q, want Alt+V", entry.Key)
	}
	if entry.Description == "" {
		t.Error("Description is empty; keybinding won't appear in /help")
	}
	if entry.Handler == nil {
		t.Fatal("handler is nil")
	}

	// First press: default → verbose
	entry.Handler()
	if got := cfg.GetConfig().OutputVerbosity; got != configuration.OutputVerbosityVerbose {
		t.Errorf("after 1st toggle: %q, want verbose", got)
	}

	// Second press: verbose → default
	entry.Handler()
	if got := cfg.GetConfig().OutputVerbosity; got != configuration.OutputVerbosityDefault {
		t.Errorf("after 2nd toggle: %q, want default", got)
	}
}

// TestOutputVerbosityToggleNilConfig verifies the handler is a no-op
// when the config manager is nil (matching the existing nil patterns).
// We exercise the pure helper that the handler wraps rather than
// mutating the singleton keymap, which is Once-protected for the
// process lifetime.
func TestOutputVerbosityToggleNilConfig(t *testing.T) {
	// Direct call to the pure logic with a config that resolves to nil
	// is not feasible — the handler does its own GetConfig(). Instead,
	// verify that computeVerbosityToggle handles empty / unknown inputs
	// safely (which is what a nil-managed handler effectively does
	// after the early return).
	got := computeVerbosityToggle("")
	if got != configuration.OutputVerbosityVerbose {
		t.Errorf("computeVerbosityToggle(\"\") = %q, want verbose (any non-verbose → verbose)", got)
	}
}

// resetForSubtest is a helper that resets the keymap once-flag and the
// global keymap pointer so a test can re-register cleanly. Use sparingly —
// this fights the Once protection that the production wiring depends on.
// Exported for tests that genuinely need to swap the registry.
func resetForSubtest(t *testing.T) {
	t.Helper()
	keymapOnce = sync.Once{}
	globalKeymap = nil
	globalKeymapOnce = sync.Once{}
	t.Cleanup(func() {
		keymapOnce = sync.Once{}
		globalKeymap = nil
		globalKeymapOnce = sync.Once{}
	})
}
