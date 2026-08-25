//go:build !js

package tools

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestAdoptableWithPasswordScanner_PromptFiresWithBPMInContext is the
// regression test for the bug where sudo's password prompt was never
// detected because the BPM check shadowed the PasswordPrompter check.
//
// This test proves that runShellCommandAdoptable (the BPM path) now
// invokes the password scanner when a PasswordPrompter is in context.
func TestAdoptableWithPasswordScanner_PromptFiresWithBPMInContext(t *testing.T) {
	prompter := &countingPrompter{password: "test-pwd-42"}

	bpm := NewBackgroundProcessManager()
	ctx := WithPasswordPrompter(context.Background(), prompter)

	// Command that writes a password prompt to stderr, reads stdin,
	// then exits. This simulates sudo's behavior.
	script := `echo "Password:" >&2; read pw; echo "got=$pw"`

	// Use a short deadline so the test doesn't hang if the prompt
	// isn't detected (the bug case — the command would block on read
	// forever and timeout-adopt as a background session).
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	output, err := runShellCommandAdoptable(ctx, script, bpm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if prompter.calls == 0 {
		t.Fatal("password prompter was not called — the password scanner " +
			"is not active in the adoptable path when a BPM is present")
	}

	// Check for redacted or raw password in the output.
	if !strings.Contains(output, "got=[REDACTED]") && !strings.Contains(output, "got=test-pwd-42") {
		t.Errorf("expected output to contain 'got=[REDACTED]' or 'got=test-pwd-42', got: %s", output)
	}
}

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
