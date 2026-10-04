package verify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// DefaultTimeout bounds a single verification check when the Runner has
// no explicit timeout: five minutes covers a normal build plus test
// suite without stalling a turn.
const DefaultTimeout = 5 * time.Minute

// Outcome is the raw outcome of executing one verification command.
type Outcome struct {
	// Passed is true when the command exited 0.
	Passed bool
	// Output is the command's combined stdout and stderr.
	Output string
	// Reason is set when the command did not complete normally
	// (timeout, cancellation); empty for a clean run, pass or fail.
	Reason string
}

// Executor executes one trusted verification command and reports its
// outcome. Implementations must honor ctx (a cancelled or timed-out
// context stops the command), bound their work, and capture combined
// output. The command string is trusted input (SP-149 §149b): it comes
// only from the starter manifest or the explicit project configuration.
type Executor interface {
	// Run executes command in dir (the project root) and returns its
	// outcome. A non-nil error marks a launch/setup failure; a non-zero
	// exit is an Outcome, not an error.
	Run(ctx context.Context, dir, command string) (Outcome, error)
}

// ShellExecutor is the default Executor: it runs the command through the
// system shell ("sh -c") in dir and captures the combined output. When
// the context expires (the Runner's per-check timeout) it reports a
// failed outcome with a reason rather than an error, so the result
// carries the partial output up to the stop.
//
// It is deliberately the only executor in this package: verification
// commands are trusted by design (SP-149 §149b), and running them under
// a shell is the mechanism the project's own build and test commands
// assume.
type ShellExecutor struct {
	// Shell is the shell the command runs through (default "sh").
	Shell string
}

// Run implements Executor.
func (e *ShellExecutor) Run(ctx context.Context, dir, command string) (Outcome, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	shell := "sh"
	if e != nil && e.Shell != "" {
		shell = e.Shell
	}
	cmd := exec.CommandContext(ctx, shell, "-c", command) //nolint:gosec // G204: the command is trusted project configuration (SP-149 149b), by design
	if dir != "" {
		cmd.Dir = dir
	}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	if ctx.Err() != nil {
		return Outcome{
			Passed: false,
			Output: buf.String(),
			Reason: "command did not finish in time (timeout or cancellation)",
		}, nil
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return Outcome{Passed: false, Output: buf.String()}, nil
		}
		return Outcome{}, fmt.Errorf("verify: run %q: %w", command, err)
	}
	return Outcome{Passed: true, Output: buf.String()}, nil
}
