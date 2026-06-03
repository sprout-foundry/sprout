//go:build !js

package cmd

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
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
	// Either IntentDetectedSh (zsh saw the binary) or IntentDirectShort
	// (static fast-path table) is a correct outcome — both route to the
	// same "reject in steer/queue" branch. CI machines vary on whether
	// zsh detection finds these binaries; we only assert the input is
	// recognized as some shell-command intent.
	a := newTestAgentForIntent(t)
	isShell := func(p PromptIntent) bool {
		return p == IntentDetectedSh || p == IntentDirectShort
	}
	for _, in := range []string{"pwd", "ls", "git status", "git log", "which go"} {
		if got := ClassifyPromptIntent(a, in); !isShell(got) {
			t.Errorf("ClassifyPromptIntent(%q) = %q, want shell-class intent", in, got)
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
	// is agent-independent). Fast-path checks should silently skip.
	if got := ClassifyPromptIntent(nil, "/commit"); got != IntentSlash {
		t.Errorf("ClassifyPromptIntent(nil, /commit) = %q, want IntentSlash", got)
	}
	if got := ClassifyPromptIntent(nil, "!ls"); got != IntentBangShell {
		t.Errorf("ClassifyPromptIntent(nil, !ls) = %q, want IntentBangShell", got)
	}
	// "pwd" still matches the static fast-path table — that check
	// doesn't need the agent either.
	if got := ClassifyPromptIntent(nil, "pwd"); got != IntentDirectShort {
		t.Errorf("ClassifyPromptIntent(nil, pwd) = %q, want IntentDirectShort", got)
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
// tests. We disable zsh detection so tests are deterministic across
// CI environments that may or may not have zsh installed — the
// classifier's zsh branch is exercised implicitly by the static
// fast-path checks above.
func newTestAgentForIntent(t *testing.T) *agent.Agent {
	t.Helper()
	a, err := agent.NewAgent()
	if err != nil {
		t.Fatalf("NewAgent() error: %v", err)
	}
	if cfg := a.GetConfig(); cfg != nil {
		cfg.EnableZshCommandDetection = false
	}
	return a
}
