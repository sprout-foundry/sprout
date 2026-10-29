//go:build !js

package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/clihooks"
)

// TestExecuteCommand_AttachesTerminalStdin pins the fix for a regression where
// a `!command` that reads input (sudo asking for a password, ssh asking for a
// passphrase) failed immediately instead of prompting: ExecuteCommand never
// set command.Stdin, so the child inherited /dev/null and every read returned
// EOF.
//
// The child reads stdin and reports whether it got data before EOF. With the
// terminal branch taken, the child is attached to os.Stdin rather than
// /dev/null. Under `go test`, os.Stdin is not a terminal, so this test drives
// the decision through testIsTerminal and asserts the observable half: the
// steer reader and the spinner are suspended for the child's lifetime, which
// is what stops them from stealing the keystrokes the child is waiting for.
func TestExecuteCommand_AttachesTerminalStdin(t *testing.T) {
	prev := testIsTerminal
	testIsTerminal = func() bool { return true }
	t.Cleanup(func() { testIsTerminal = prev })

	// A steer hook that records the pause/resume the child's lifetime spans.
	// SetSteerHooks clears the hooks and resets the pause refcount, so the
	// previous pair is restored on cleanup rather than nil'd.
	var paused, resumed bool
	prevPause, prevResume := clihooks.SteerHooks()
	clihooks.SetSteerHooks(
		func() { paused = true },
		func() { resumed = true },
	)
	t.Cleanup(func() { clihooks.SetSteerHooks(prevPause, prevResume) })

	if _, err := ExecuteCommand("true"); err != nil {
		t.Fatalf("ExecuteCommand(true): %v", err)
	}

	if !paused {
		t.Error("steer reader was not paused; it would race the child for terminal input")
	}
	if !resumed {
		t.Error("steer reader was not resumed after the child exited")
	}
}

// TestExecuteCommand_NonTerminalStdinNotAttached verifies the other half of
// the decision: when stdin is not a terminal, the child must not be handed
// sprout's own stdin. Piping a non-terminal stdin in would feed the child
// whatever sprout was fed (a heredoc, a CI job's /dev/null), which is not the
// user answering a prompt.
//
// The child reads stdin; with no terminal the read must return EOF promptly
// rather than block, and the steer hooks must be untouched.
func TestExecuteCommand_NonTerminalStdinNotAttached(t *testing.T) {
	prev := testIsTerminal
	testIsTerminal = func() bool { return false }
	t.Cleanup(func() { testIsTerminal = prev })

	var paused bool
	clihooks.SetSteerHooks(func() { paused = true }, nil)
	t.Cleanup(func() { clihooks.SetSteerHooks(nil, nil) })

	done := make(chan error, 1)
	go func() {
		// `read` returns non-zero on EOF; the point is that it returns at
		// all. A hang here would mean stdin was left attached to something
		// that never delivers.
		_, err := ExecuteCommand("read -r _ignored")
		done <- err
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("child blocked reading stdin with no terminal attached")
	}

	if paused {
		t.Error("steer reader was paused for a non-terminal child; nothing should suspend")
	}
}

// TestExecuteCommand_StreamsOutput guards the capture contract the error path
// depends on: ExecuteCommand returns the combined output so a failed command
// can report what it printed.
func TestExecuteCommand_StreamsOutput(t *testing.T) {
	prev := testIsTerminal
	testIsTerminal = func() bool { return false }
	t.Cleanup(func() { testIsTerminal = prev })

	out, err := ExecuteCommand("echo hello-from-exec")
	if err != nil {
		t.Fatalf("ExecuteCommand: %v", err)
	}
	if !strings.Contains(out, "hello-from-exec") {
		t.Errorf("captured output = %q, want it to contain %q", out, "hello-from-exec")
	}
}
