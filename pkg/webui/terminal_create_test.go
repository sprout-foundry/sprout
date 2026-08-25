//go:build !js

package webui

import (
	"os"
	"strings"
	"testing"

	"github.com/creack/pty"
)

func TestValidateSessionID_Create(t *testing.T) {
	// Valid IDs
	validIDs := []string{
		"abc",
		"ABC",
		"123",
		"abc-123",
		"abc_123",
		"abc.123",
		"a",
		"my-session_id.test",
	}
	for _, id := range validIDs {
		if err := validateSessionID(id); err != nil {
			t.Errorf("validateSessionID(%q) should be valid, got error: %v", id, err)
		}
	}

	// Empty error
	err := validateSessionID("")
	if err == nil {
		t.Error("validateSessionID(\"\") should return error")
	} else if !strings.Contains(err.Error(), "required") {
		t.Errorf("validateSessionID(\"\") error = %q; should mention required", err.Error())
	}

	// >128 characters error
	longID := strings.Repeat("a", 129)
	err = validateSessionID(longID)
	if err == nil {
		t.Error("validateSessionID(129 chars) should return error")
	} else if !strings.Contains(err.Error(), "128") {
		t.Errorf("validateSessionID(129 chars) error = %q; should mention max 128", err.Error())
	}

	// Invalid characters error
	invalidIDs := []string{
		"abc def", // space
		"abc/def", // slash
		"abc:def", // colon
		"abc@def", // at
		"abc#def", // hash
		"abc!def", // exclamation
	}
	for _, id := range invalidIDs {
		err = validateSessionID(id)
		if err == nil {
			t.Errorf("validateSessionID(%q) should return error", id)
		}
	}

	// Boundary: exactly 128 characters should be valid
	boundaryID := strings.Repeat("a", 128)
	err = validateSessionID(boundaryID)
	if err != nil {
		t.Errorf("validateSessionID(128 chars) should be valid, got error: %v", err)
	}
}

func TestResolveShellArgs(t *testing.T) {
	tests := []struct {
		name     string
		shell    string
		expected []string
	}{
		{"bash", "bash", []string{"--login"}},
		{"zsh", "zsh", []string{"--login"}},
		{"sh", "sh", nil},
		{"fish", "fish", nil},
		{"/bin/bash", "/bin/bash", []string{"--login"}},
		{"/usr/bin/zsh", "/usr/bin/zsh", []string{"--login"}},
		{"unknown shell", "unknown-shell", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveShellArgs(tt.shell)
			if len(got) != len(tt.expected) {
				t.Errorf("resolveShellArgs(%q) = %v; want %v", tt.shell, got, tt.expected)
			} else if len(got) > 0 && got[0] != tt.expected[0] {
				t.Errorf("resolveShellArgs(%q) = %v; want %v", tt.shell, got, tt.expected)
			}
		})
	}
}

func TestWithName(t *testing.T) {
	s := &TerminalSession{}
	opt := WithName("my terminal")
	opt(s)
	if s.Name != "my terminal" {
		t.Errorf("WithName: name = %q; want %q", s.Name, "my terminal")
	}
}

func TestWithName_TrimsWhitespace(t *testing.T) {
	s := &TerminalSession{}
	opt := WithName("  my terminal  ")
	opt(s)
	if s.Name != "my terminal" {
		t.Errorf("WithName: name = %q; want %q", s.Name, "my terminal")
	}
}

func TestWithAutoClose(t *testing.T) {
	s := &TerminalSession{}
	opt := WithAutoClose(true)
	opt(s)
	if !s.AutoClose {
		t.Error("WithAutoClose(true): AutoClose should be true")
	}

	s2 := &TerminalSession{AutoClose: true}
	opt2 := WithAutoClose(false)
	opt2(s2)
	if s2.AutoClose {
		t.Error("WithAutoClose(false): AutoClose should be false")
	}
}

// hasEnvNamed reports whether the env slice contains an entry whose name
// (substring before '=') equals name.
func hasEnvNamed(env []string, name string) bool {
	for _, entry := range env {
		if idx := strings.IndexByte(entry, '='); idx >= 0 {
			if entry[:idx] == name {
				return true
			}
		}
	}
	return false
}

// firstEnvValue returns the value of the first env entry whose name matches
// name, or "" if no such entry exists.
func firstEnvValue(env []string, name string) string {
	for _, entry := range env {
		if idx := strings.IndexByte(entry, '='); idx >= 0 {
			if entry[:idx] == name {
				return entry[idx+1:]
			}
		}
	}
	return ""
}

// withEnv sets the named env var for the duration of the test (restored via t.Cleanup).
func withEnv(t *testing.T, key, value string) {
	t.Helper()
	prev, had := os.LookupEnv(key)
	if err := os.Setenv(key, value); err != nil {
		t.Fatalf("os.Setenv(%q): %v", key, err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, prev)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

// unsetEnv removes the named env var for the duration of the test.
func unsetEnv(t *testing.T, key string) {
	t.Helper()
	prev, had := os.LookupEnv(key)
	if had {
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("os.Unsetenv(%q): %v", key, err)
		}
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, prev)
		}
	})
}

// ---------------------------------------------------------------------------
// Regression: NO_COLOR / FORCE_COLOR must NOT leak from sprout's process env
// into the user's interactive shell spawned by the webui embedded terminal.
//
// Background: cmd/agent_modes.go RunAgent auto-sets NO_COLOR=1 when stdout is
// not a TTY (to keep ANSI out of rotated daemon logs). os.Environ() exposes
// that var to every spawn that does append(os.Environ(), ...). The webui
// embedded terminal hands its own env to the user's shell — without an
// explicit strip, the user's shell (and any JS tools it runs that internally
// set FORCE_COLOR=1) sees NO_COLOR=1 and node emits a "NO_COLOR env is
// ignored due to FORCE_COLOR env being set" warning, polluting the terminal.
// ---------------------------------------------------------------------------

func TestBuildTerminalEnv_StripsNoColorAndForceColorFromParent(t *testing.T) {
	withEnv(t, "NO_COLOR", "1")
	withEnv(t, "FORCE_COLOR", "1")

	size := &pty.Winsize{Rows: 24, Cols: 80}
	env := buildTerminalEnv("/bin/bash", size)

	if hasEnvNamed(env, "NO_COLOR") {
		t.Errorf("terminal env must not contain NO_COLOR (leak from sprout log-rotation env); got entry: %q",
			firstEnvValue(env, "NO_COLOR"))
	}
	if hasEnvNamed(env, "FORCE_COLOR") {
		t.Errorf("terminal env must not contain FORCE_COLOR (should come from user's shell rc, not sprout); got entry: %q",
			firstEnvValue(env, "FORCE_COLOR"))
	}
}

func TestBuildTerminalEnv_StripsWhenNeitherSet(t *testing.T) {
	// Ensure neither var is present before calling the helper, so we verify
	// the strip pass is harmless when there's nothing to strip.
	unsetEnv(t, "NO_COLOR")
	unsetEnv(t, "FORCE_COLOR")

	size := &pty.Winsize{Rows: 24, Cols: 80}
	env := buildTerminalEnv("/bin/bash", size)

	if hasEnvNamed(env, "NO_COLOR") {
		t.Error("terminal env unexpectedly contains NO_COLOR")
	}
	if hasEnvNamed(env, "FORCE_COLOR") {
		t.Error("terminal env unexpectedly contains FORCE_COLOR")
	}
}

func TestBuildTerminalEnv_StripsOnlyColorVars_PreservesOtherVars(t *testing.T) {
	withEnv(t, "NO_COLOR", "1")
	withEnv(t, "FORCE_COLOR", "1")
	withEnv(t, "SPROUT_TEST_KEEP_VAR", "should-survive")

	size := &pty.Winsize{Rows: 24, Cols: 80}
	env := buildTerminalEnv("/bin/bash", size)

	if !hasEnvNamed(env, "SPROUT_TEST_KEEP_VAR") {
		t.Error("terminal env must preserve other parent vars (only NO_COLOR/FORCE_COLOR are stripped)")
	}
	if hasEnvNamed(env, "NO_COLOR") || hasEnvNamed(env, "FORCE_COLOR") {
		t.Error("terminal env must still strip NO_COLOR and FORCE_COLOR")
	}
}

func TestBuildTerminalEnv_PreservesTerminalEnvVars(t *testing.T) {
	// Use a deterministic shell path so the test doesn't depend on the
	// host's $SHELL — which is meaningful only to resolveShell, not to
	// buildTerminalEnv itself.
	size := &pty.Winsize{Rows: 30, Cols: 132}
	const fakeShell = "/bin/test-shell"
	env := buildTerminalEnv(fakeShell, size)

	mustContain := map[string]string{
		"TERM":                "xterm-256color",
		"COLORTERM":           "truecolor",
		"SHELL":               fakeShell,
		"SPROUT_WEB_TERMINAL": "1",
		"COLUMNS":             "132",
		"LINES":               "30",
	}
	for name, expected := range mustContain {
		got := firstEnvValue(env, name)
		if got != expected {
			t.Errorf("terminal env missing %s=%s; got %q", name, expected, got)
		}
	}
}

func TestStripEnvVars_Basic(t *testing.T) {
	env := []string{
		"PATH=/usr/bin",
		"NO_COLOR=1",
		"HOME=/home/u",
		"FORCE_COLOR=1",
		"USER=u",
		"no_color=lowercase", // must NOT be stripped (case-sensitive per env(7))
	}
	out := stripEnvVars(env, []string{"NO_COLOR", "FORCE_COLOR"})

	if hasEnvNamed(out, "NO_COLOR") || hasEnvNamed(out, "FORCE_COLOR") {
		t.Errorf("expected NO_COLOR and FORCE_COLOR stripped; got %v", out)
	}
	for _, mustKeep := range []string{"PATH", "HOME", "USER", "no_color"} {
		if !hasEnvNamed(out, mustKeep) {
			t.Errorf("stripEnvVars unexpectedly removed %q; got %v", mustKeep, out)
		}
	}
}

func TestStripEnvVars_EmptyInputs(t *testing.T) {
	// Empty toStrip → identity (returns input slice).
	got := stripEnvVars([]string{"PATH=/x", "HOME=/h"}, nil)
	if len(got) != 2 || got[0] != "PATH=/x" || got[1] != "HOME=/h" {
		t.Errorf("stripEnvVars with nil toStrip should return input unchanged; got %v", got)
	}
	// Empty env → empty.
	got = stripEnvVars(nil, []string{"NO_COLOR"})
	if len(got) != 0 {
		t.Errorf("stripEnvVars with empty env should return empty; got %v", got)
	}
}

func TestStripEnvVars_DoesNotAliasInput(t *testing.T) {
	env := []string{"NO_COLOR=1", "PATH=/x"}
	out := stripEnvVars(env, []string{"NO_COLOR"})
	// stripEnvVars must use an independent backing array (env[:0:0] zero-cap
	// re-slice). Verify by mutating the returned slice and confirming the
	// caller's slice is unchanged.
	if len(out) == len(env) {
		t.Fatalf("expected NO_COLOR to be stripped; got %v", out)
	}
	if !hasEnvNamed(env, "NO_COLOR") {
		t.Error("stripEnvVars mutated the input env slice (lost NO_COLOR from caller)")
	}
	// Mutating out[0] must not affect env — confirms independent backing array.
	out[0] = "MUTATED=1"
	if hasEnvNamed(env, "MUTATED") {
		t.Error("stripEnvVars returns a slice that aliases the caller's backing array")
	}
}
