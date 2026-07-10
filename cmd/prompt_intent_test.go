//go:build !js

package cmd

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

func TestClassifyPromptIntent_Slash(t *testing.T) {
	a := newTestAgentForIntent(t)
	for _, in := range []string{"/commit", "  /persona acid", "/help"} {
		if got := ClassifyPromptIntent(a, in); got != IntentSlash {
			t.Errorf("ClassifyPromptIntent(%q) = %q, want %q", in, got, IntentSlash)
		}
	}
}

func TestClassifyPromptIntent_BangShell(t *testing.T) {
	a := newTestAgentForIntent(t)
	for _, in := range []string{"!ls", "!  pwd", "!git status"} {
		if got := ClassifyPromptIntent(a, in); got != IntentBangShell {
			t.Errorf("ClassifyPromptIntent(%q) = %q, want %q", in, got, IntentBangShell)
		}
	}
}

func TestClassifyPromptIntent_ShellShortcut(t *testing.T) {
	// With the static fast-path table removed, shell shortcuts like "pwd"
	// or "ls" are no longer intercepted by ClassifyPromptIntent unless
	// zsh detection catches them. In the test environment zsh detection
	// is disabled (see newTestAgentForIntent), so these should classify
	// as IntentNone (freeform) — the REPL will let the agent handle them
	// normally.
	a := newTestAgentForIntent(t)
	for _, in := range []string{"pwd", "ls", "git status", "git log", "which go"} {
		if got := ClassifyPromptIntent(a, in); got != IntentNone {
			t.Errorf("ClassifyPromptIntent(%q) = %q, want IntentNone", in, got)
		}
	}
}

func TestClassifyPromptIntent_Freeform(t *testing.T) {
	a := newTestAgentForIntent(t)
	for _, in := range []string{
		"please refactor the auth middleware",
		"what does this function do?",
		"explain the steer coordinator",
		"",
		"   ",
	} {
		if got := ClassifyPromptIntent(a, in); got != IntentNone {
			t.Errorf("ClassifyPromptIntent(%q) = %q, want IntentNone", in, got)
		}
	}
}

func TestClassifyPromptIntent_NilAgent(t *testing.T) {
	// Slash detection must work without an agent (the registry check
	// is agent-independent). The zsh detection branch and static fast-path
	// checks are silently skipped without an agent, so shell-class text
	// returns IntentNone.
	if got := ClassifyPromptIntent(nil, "/commit"); got != IntentSlash {
		t.Errorf("ClassifyPromptIntent(nil, /commit) = %q, want IntentSlash", got)
	}
	if got := ClassifyPromptIntent(nil, "!ls"); got != IntentBangShell {
		t.Errorf("ClassifyPromptIntent(nil, !ls) = %q, want IntentBangShell", got)
	}
	// Without an agent, zsh detection is skipped. "pwd" is no longer
	// intercepted by the removed static fast-path table, so it falls
	// through as freeform.
	if got := ClassifyPromptIntent(nil, "pwd"); got != IntentNone {
		t.Errorf("ClassifyPromptIntent(nil, pwd) = %q, want IntentNone", got)
	}
	if got := ClassifyPromptIntent(nil, "what does foo do?"); got != IntentNone {
		t.Errorf("ClassifyPromptIntent(nil, plain text) = %q, want IntentNone", got)
	}
}

func TestClassifyPromptIntent_BangWithoutPayloadIsFreeform(t *testing.T) {
	// IsSlashCommand rejects a bare "!" with no following command; the
	// classifier must treat it as freeform rather than mis-labeling it.
	a := newTestAgentForIntent(t)
	for _, in := range []string{"!", "!   "} {
		if got := ClassifyPromptIntent(a, in); got != IntentNone {
			t.Errorf("ClassifyPromptIntent(%q) = %q, want IntentNone", in, got)
		}
	}
}

// newTestAgentForIntent builds a minimal Agent suitable for classifier
// tests. We disable zsh detection via the manager so the mutation
// sticks (GetConfig returns a clone, so setting on the returned pointer
// is a no-op). With zsh disabled, the classifier has no shell-command
// interception — tests can assert IntentNone for shell-like text.
func newTestAgentForIntent(t *testing.T) *agent.Agent {
	t.Helper()
	a, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent() error: %v", err)
	}
	if mgr := a.GetConfigManager(); mgr != nil {
		if err := mgr.UpdateConfigNoSave(func(c *configuration.Config) error {
			c.EnableZshCommandDetection = false
			return nil
		}); err != nil {
			t.Fatalf("failed to disable zsh detection: %v", err)
		}
	}
	return a
}
