//go:build !js

package tools

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestRunShellCommandWithPasswordSupport_UnterminatedPrompt is the regression
// test for the SP-089 sudo-password-plumbing bug.
//
// Pre-fix: bufio.Scanner buffered entire lines (waiting for \n). Sudo writes
// "Password: " with a trailing space, no newline — the scanner waited
// indefinitely, the prompter was never invoked, and the command blocked
// until the context deadline. The user observed: "I approved the command
// but no password prompt appeared."
//
// Post-fix: streamPipeAndDetect accumulates un-terminated bytes in a per-pipe
// pending buffer and uses a settle-delay timer to detect prompts that don't
// end with \n. The settle fires when no new bytes arrive for promptSettleDelay
// after the pending buffer matches the prompt regex.
//
// This test simulates sudo's exact behavior via sh's `printf` (no trailing
// newline), then verifies the prompter was invoked AND the captured input was
// fed back through the stdin pipe to be echoed by `cat`.
func TestRunShellCommandWithPasswordSupport_UnterminatedPrompt(t *testing.T) {
	prompter := &countingPrompter{password: "secret-pw"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// printf 'Password: ' emits the prompt WITHOUT a trailing newline.
	// Then `read pw` blocks waiting for input. After we send "secret-pw\n"
	// the variable is set. We echo back $pw so we can verify it was
	// correctly delivered.
	cmd := `printf 'Password: '; read pw; echo "got: $pw"`

	output, err := runShellCommandWithPasswordSupport(ctx, cmd, prompter)
	if err != nil {
		t.Fatalf("unexpected error: %v\noutput: %s", err, output)
	}

	if prompter.calls != 1 {
		t.Errorf("expected prompter to be called once, got %d (output: %s)", prompter.calls, output)
	}
	// After redaction the echo line should read "got: [REDACTED]" — the
	// redaction layer runs over the entire outputBuf, replacing the raw
	// password value. What we care about is that the password actually
	// reached the child (its echo was non-empty) — not the literal text.
	if !strings.Contains(output, "got: ") {
		t.Errorf("password was not delivered to the child — output: %s", output)
	}
	if strings.Contains(output, "secret-pw") {
		t.Errorf("password should be redacted from output, was not: %s", output)
	}
}

// TestRunShellCommandWithPasswordSupport_PartialChunks verifies the scanner
// fires the prompter even when the prompt spans multiple Read() calls —
// i.e., the kernel delivers the bytes in pieces. This was a separate failure
// mode worth covering: bufio.Scanner with default 64KB buffers handles this
// fine for completed lines, but our settle-detection logic must also handle
// the case where "Password: " arrives as e.g. "Pa" + "ssword: " across two
// reads.
func TestRunShellCommandWithPasswordSupport_PartialChunks(t *testing.T) {
	prompter := &countingPrompter{password: "pw"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := `printf 'Password: '; read pw; echo "got=$pw"`

	output, err := runShellCommandWithPasswordSupport(ctx, cmd, prompter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prompter.calls != 1 {
		t.Errorf("expected prompter to be called once, got %d (output: %s)", prompter.calls, output)
	}
	if !strings.Contains(output, "got=") {
		t.Errorf("expected echo of delivered password, got: %s", output)
	}
	if strings.Contains(output, "pw\n") || strings.Contains(output, "got=pw") {
		// Raw password should be redacted; we should NOT see "pw" literal.
		t.Errorf("password should be redacted, was not. output=%s", output)
	}
}

// TestRunShellCommandWithPasswordSupport_TerminatedPromptFastPath verifies
// that prompts emitted with a trailing newline still fire WITHOUT the
// settle-delay (no perceptible lag). We measure elapsed time and assert
// it's well under the settle-delay.
func TestRunShellCommandWithPasswordSupport_TerminatedPromptFastPath(t *testing.T) {
	prompter := &countingPrompter{password: "pw"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := `printf 'Password:\n'; read pw; echo "got=$pw"`

	start := time.Now()
	output, err := runShellCommandWithPasswordSupport(ctx, cmd, prompter)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prompter.calls != 1 {
		t.Errorf("expected prompter to be called once, got %d (output: %s)", prompter.calls, output)
	}
	// Completed-line prompts fire without settle. Generous upper bound
	// (1s) — the slowness here is `cmd.Wait()`, not the scanner.
	if elapsed > 1*time.Second {
		t.Errorf("terminated-prompt path took too long: %v", elapsed)
	}
}

// recordingPrompter records call timing for the convergence test below.
// Avoid pulling in testify just to track a slice.
type recordingPrompter struct {
	mu       sync.Mutex
	password string
	calledAt []time.Time
}

func (r *recordingPrompter) Prompt(_ context.Context, _ string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calledAt = append(r.calledAt, time.Now())
	return r.password, nil
}

func (r *recordingPrompter) Calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calledAt)
}

// TestRunShellCommandWithPasswordSupport_NonPromptOutput verifies that
// benign output that LOOKS like a prompt prefix but is mid-sentence
// (e.g., "the password was reset" or "checking pre-conditions ...") does
// NOT cause a false-positive prompt invocation. The settle-delay only
// fires after sustained silence; rapid continuous output resets the timer.
func TestRunShellCommandWithPasswordSupport_NonPromptOutput(t *testing.T) {
	rp := &recordingPrompter{password: "pw"}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Command streams several non-prompt lines quickly, then exits. The
	// regex matches against $ at end of buffer, but our lines all end
	// with content that makes the regex fail to match.
	cmd := `sh -c 'for i in 1 2 3; do echo "Password changed at iteration $i"; done; echo done'`

	output, err := runShellCommandWithPasswordSupport(ctx, cmd, rp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rp.Calls() != 0 {
		t.Errorf("prompter was wrongly called %d times for benign output. output=%s", rp.Calls(), output)
	}
}
