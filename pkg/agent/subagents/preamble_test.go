package subagents

import (
	"strings"
	"testing"
)

// TestAppendSubagentPreamble verifies the shared subagent operating rules
// are appended to persona prompts exactly once, and that the append is
// idempotent for prompts that already carry the marker.
func TestAppendSubagentPreamble(t *testing.T) {
	base := "# Reviewer Subagent\n\nYou are a code-review specialist.\n"

	got := AppendSubagentPreamble(base)
	if !strings.Contains(got, "## Subagent Operating Rules (framework)") {
		t.Fatal("preamble not appended")
	}
	if !strings.Contains(got, "Do NOT commit or push") {
		t.Error("preamble missing git constraint")
	}
	if !strings.Contains(got, "you cannot spawn further subagents") {
		t.Error("preamble missing no-nested-subagents constraint")
	}
	if !strings.Contains(got, "do NOT retry or work around it") {
		t.Error("preamble missing security-error reporting constraint")
	}
	// The base prompt must be preserved verbatim at the top.
	if !strings.HasPrefix(got, base) {
		t.Errorf("base prompt altered by preamble append; got prefix: %q", got[:min(len(got), len(base))])
	}
	// No stray trailing blank lines between base and preamble.
	if strings.Contains(got, "\n\n\n") {
		t.Error("preamble append introduced double blank lines")
	}

	// Idempotent: appending twice must not duplicate the section.
	once := AppendSubagentPreamble(base)
	twice := AppendSubagentPreamble(once)
	if twice != once {
		t.Error("AppendSubagentPreamble is not idempotent")
	}
	if strings.Count(twice, "## Subagent Operating Rules (framework)") != 1 {
		t.Error("preamble duplicated")
	}

	// Also idempotent when the marker is at the start (defensive).
	if got := AppendSubagentPreamble("## Subagent Operating Rules (framework)\n\nx"); got != "## Subagent Operating Rules (framework)\n\nx" {
		t.Error("prompt already carrying the marker must be returned unchanged")
	}
}

// TestAppendSubagentPreambleForPersona_GitWrite verifies the git-write
// variant: the incremental-commit rule replaces the "do NOT commit" rule
// (a deadline must cost at most the last committed slice), and the rest of
// the preamble is unchanged.
func TestAppendSubagentPreambleForPersona_GitWrite(t *testing.T) {
	base := "# Orchestrator Subagent\n"

	got := AppendSubagentPreambleForPersona(base, true)
	if !strings.Contains(got, "Commit incrementally") {
		t.Errorf("git-write preamble missing incremental-commit rule:\n%s", got)
	}
	if strings.Contains(got, "Do NOT commit or push") {
		t.Errorf("git-write preamble must not carry the read-only commit ban:\n%s", got)
	}
	if !strings.Contains(got, "you cannot spawn further subagents") {
		t.Error("git-write preamble lost the shared constraints")
	}
	if !strings.Contains(got, "never force flags") {
		t.Error("git-write preamble lost the destructive-flag ban")
	}

	// The read-only variant must not leak the write rule.
	ro := AppendSubagentPreambleForPersona(base, false)
	if strings.Contains(ro, "Commit incrementally") {
		t.Errorf("read-only preamble must not carry the commit rule:\n%s", ro)
	}

	// Idempotent in both variants.
	if AppendSubagentPreambleForPersona(got, true) != got {
		t.Error("git-write append is not idempotent")
	}
}
