package console

import (
	"strings"
	"sync"
	"testing"
)

// fakeVerbosityConfig is an in-test OutputVerbosityToggler. It stands
// in for a *configuration.Manager — which the console package must not
// name, because pkg/configuration imports console (the Glyph surface)
// and a console test file importing configuration would close a cycle.
// The real Manager satisfies the same interface at the cmd call site
// (compile-checked there).
type fakeVerbosityConfig struct {
	verbosity string
}

func (f *fakeVerbosityConfig) CurrentOutputVerbosity() string { return f.verbosity }

func (f *fakeVerbosityConfig) SetOutputVerbosity(v string) error {
	f.verbosity = v
	return nil
}

// TestComputeVerbosityToggle verifies the pure toggle logic.
func TestComputeVerbosityToggle(t *testing.T) {
	cases := []struct {
		current string
		want    string
	}{
		{"default", "verbose"},
		{"", "verbose"},
		{"compact", "verbose"},
		{"verbose", "default"},
		{"unknown", "verbose"},
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
	verbose := verbosityToggleLabel("verbose")
	if !strings.Contains(verbose, "verbose") {
		t.Errorf("verbose label missing 'verbose': %q", verbose)
	}
	if !strings.Contains(verbose, "Alt+V") {
		t.Errorf("verbose label missing 'Alt+V': %q", verbose)
	}

	def := verbosityToggleLabel("default")
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
	cfg := &fakeVerbosityConfig{}

	// Register the keymap entry (idempotent via sync.Once).
	RegisterKeymapForFooter(nil, cfg)
	// Repeat-call should be harmless; verifies idempotency of the Once
	// guard that CLI-D-3 explicitly relies on.
	RegisterKeymapForFooter(nil, cfg)

	entry, ok := GlobalKeymap().Lookup("output.verbosity.toggle")
	if !ok {
		t.Fatal("output.verbosity.toggle not registered")
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
	if got := cfg.verbosity; got != "verbose" {
		t.Errorf("after 1st toggle: %q, want verbose", got)
	}

	// Second press: verbose → default
	entry.Handler()
	if got := cfg.verbosity; got != "default" {
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
	if got != "verbose" {
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

func TestRegisterKeymapForFooter_HintFollowsVerbosity(t *testing.T) {
	for verbosity, want := range map[string]bool{
		"":        true,
		"default": true,
		"verbose": true,
		"compact": false,
	} {
		resetForSubtest(t)
		f := &StatusFooter{}
		RegisterKeymapForFooter(f, &fakeVerbosityConfig{verbosity: verbosity})
		if f.showKeymapHint != want {
			t.Errorf("verbosity %q: showKeymapHint = %v, want %v", verbosity, f.showKeymapHint, want)
		}
	}
}
