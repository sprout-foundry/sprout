//go:build !js

package tools

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestAdoptableWithPasswordScanner_PromptFiresWithBPMInContext was
// removed: it raced on CI runners (stderr prompt vs. scanner read vs.
// stdin write under load — macOS failed intermittently with only
// "Password:" in the output). The prompt-detect-and-answer behavior it
// guarded is covered deterministically by the shell_native_password_*
// suites; the BPM-in-context path is covered by the NoPrompter and
// LongCommandAdoptsOnTimeout tests below.

// TestAdoptableWithPasswordScanner_NoPrompterStillWorks ensures that
// when no prompter is registered, the adoptable path falls back to the
// original direct-writer behavior (no pipes, no scanner).
func TestAdoptableWithPasswordScanner_NoPrompterStillWorks(t *testing.T) {
	bpm := NewBackgroundProcessManager()
	ctx := context.Background()

	output, err := runShellCommandAdoptable(ctx, "echo no-prompter-here", bpm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(output, "no-prompter-here") {
		t.Errorf("expected output to contain 'no-prompter-here', got: %s", output)
	}
}

// TestAdoptableWithPasswordScanner_LongCommandAdoptsOnTimeout verifies
// that a long-running command still gets adopted as a background session
// on timeout, even when a password prompter is in context. This proves
// the merge didn't break background adoption.
func TestAdoptableWithPasswordScanner_LongCommandAdoptsOnTimeout(t *testing.T) {
	prompter := &countingPrompter{password: "unused"}
	bpm := NewBackgroundProcessManager()
	ctx := WithPasswordPrompter(context.Background(), prompter)

	// Short deadline so the command promotes quickly.
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	output, err := runShellCommandAdoptable(ctx, "sleep 30", bpm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(output, "bg-") {
		t.Errorf("expected background promotion message with bg- session id, got: %s", output)
	}

	// Prompter should NOT have been called — sleep doesn't prompt.
	if prompter.calls != 0 {
		t.Errorf("prompter should not have been called for sleep, got %d calls", prompter.calls)
	}
}
