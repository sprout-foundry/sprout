//go:build !js

package tools

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestRunShellCommandWithPasswordSupport_DoublePromptSingleCall locks in
// the fix for the double-prompt race in handlePrompt.
//
// Regression: previously, the stdout and stderr scanner goroutines could
// both detect the same "Password:" prompt concurrently and both pass the
// `attempts >= maxPasswordAttempts` guard before either set stdinClosed.
// Result: prompter called twice, attempts counter inflated, user saw two
// password prompts in rapid succession.
//
// Fix: handlePrompt uses a separate `promptClaimed` flag to gate the
// call to prompter.Prompt() under mu — first-writer wins, concurrent
// detections bail. Stdin closure is deferred to the outcome branch
// (success / error / cap-reached) because the success path needs to
// write the response to stdin before closing.
func TestRunShellCommandWithPasswordSupport_DoublePromptSingleCall(t *testing.T) {
	prompter := &countingPrompter{password: "shared-secret"}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Write "Password:" to BOTH stdout and stderr from the same command,
	// then read stdin (consume the prompter's response). Both pipes
	// should detect the prompt and race into handlePrompt. Only ONE
	// should call the prompter.
	cmd := `sh -c 'echo Password:; echo Password: 1>&2; read pw; echo "got=$pw"'`

	out, err := runShellCommandWithPasswordSupport(ctx, cmd, prompter)
	if err != nil {
		t.Logf("wrapper returned err=%v", err)
	}

	if prompter.calls != 1 {
		t.Errorf("BUG REGRESSION: prompter called %d times for one prompt (expected 1). output=%q",
			prompter.calls, out)
	}

	if !strings.Contains(out, "got=[REDACTED]") {
		t.Errorf("password should be redacted; output=%q", out)
	}
}
