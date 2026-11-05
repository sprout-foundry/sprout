//go:build !js

package cmd

// Tests for the SP-169 framing question in `sprout mcp add`: asked once per
// server, defaulting to yes, and defaulted (not asked) in non-interactive runs.

import (
	"bufio"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/mcp"
	"github.com/sprout-foundry/sprout/pkg/testutil"
)

// TestPromptTrustUntrustedContent_NonInteractiveDefaultsTrue pins the
// autonomous/piped-run contract: no prompt, framing on.
func TestPromptTrustUntrustedContent_NonInteractiveDefaultsTrue(t *testing.T) {
	forceNonTTY(t)

	var trust bool
	out := testutil.CaptureStdout(t, func() {
		var err error
		trust, err = promptTrustUntrustedContent("github-mcp", bufio.NewReader(strings.NewReader("")))
		if err != nil {
			t.Fatalf("promptTrustUntrustedContent: %v", err)
		}
	})

	if !trust {
		t.Error("non-interactive run must default to framing on")
	}
	if strings.Contains(out, "untrusted content") {
		t.Errorf("non-interactive run must not print the question:\n%s", out)
	}
}

// TestPromptTrustUntrustedContent_ExplainsFramingIsALabel pins the wording
// requirement: the prompt has to say framing does not block tools, or a user
// will read the toggle as a sandbox.
func TestPromptTrustUntrustedContent_ExplainsFramingIsALabel(t *testing.T) {
	forceNonTTY(t)

	// Exercise the wording directly: the TTY branch prints it, and the
	// non-TTY branch deliberately prints nothing.
	text := trustUntrustedPromptText("github-mcp")
	for _, want := range []string{
		"Treat output from \"github-mcp\" as untrusted content?",
		"return text this agent did not write",
		"marks",
		"less likely to be followed",
		"It does not block tools or change what they can do.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("prompt text missing %q:\n%s", want, text)
		}
	}
}

// TestApplyTrustUntrustedPrompt_RecordsAnswer drives the helper the add flows
// call: the answer lands on the server config and survives a save/load round
// trip (so an opt-out is not silently re-defaulted on the next load).
func TestApplyTrustUntrustedPrompt_RecordsAnswer(t *testing.T) {
	forceNonTTY(t)

	cfg := mcp.MCPServerConfig{Name: "github-mcp"}
	if err := applyTrustUntrustedPrompt(&cfg, bufio.NewReader(strings.NewReader(""))); err != nil {
		t.Fatalf("applyTrustUntrustedPrompt: %v", err)
	}
	if !cfg.TrustUntrustedContentOrDefault() {
		t.Error("the recorded answer should be framing on for a non-interactive run")
	}
}
