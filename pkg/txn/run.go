package txn

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// RunCommand executes shape #2: `command` runs under /bin/sh -c with the
// workdir as cwd and the inherited environment, and its two output streams
// are captured separately, each capped to the last 256 KiB.
//
// The timeout is the ONLY canceller. ctx is deliberately not wired into the
// subprocess: a client hangup (or a cancelled request) must not SIGKILL a
// command mid-write, so the daemon calls this under context.WithoutCancel
// and the process group is killed only when the timeout fires. Killing the
// group — not just the shell — is what makes compiler children die with it.
//
// Every outcome is reportable: a non-zero exit, a timeout (exit_code 124,
// timed_out true) and even a failure to start (exit_code 126, the error in
// stderr) come back as a result, never as a Go error. Only an unresolvable
// workdir returns one.
func RunCommand(ctx context.Context, workdir string, request RunRequest) (RunResult, error) {
	result := RunResult{}

	dir, err := resolveWorkdir(workdir)
	if err != nil {
		return result, err
	}
	dir, err = resolveRunWorkdir(dir, request.Workdir)
	if err != nil {
		return result, err
	}

	timeout := normalizeTimeout(request.TimeoutSeconds)
	stdout, stderr := newRollingBuffer(MaxOutputBytes), newRollingBuffer(MaxOutputBytes)

	cmd := exec.Command(shellPath(), "-c", request.Command)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	// New process group so the whole tree (shell + compiler children) can
	// be killed at once on timeout.
	setTxnProcessGroup(cmd)

	started := time.Now()
	if err := cmd.Start(); err != nil {
		result.ExitCode = StartFailureExitCode
		result.Stderr = fmt.Sprintf("txn: start %q in %s: %v", request.Command, dir, err)
		result.DurationMs = time.Since(started).Milliseconds()
		return result, nil
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case err := <-done:
		result.ExitCode = exitCodeOf(err, cmd)
	case <-timer.C:
		killTxnProcessGroup(cmd)
		err := <-done // reap, and drain the last output the group wrote
		result.TimedOut = true
		result.ExitCode = TimeoutExitCode
		_ = err
	}

	result.DurationMs = time.Since(started).Milliseconds()
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()
	result.Truncated = stdout.truncated() || stderr.truncated()
	return result, nil
}

// exitCodeOf extracts the process's exit status. A signal death reports -1
// (Go's ExitCode convention for "no exit status"), which the platform can
// distinguish from a genuine 124 timeout by timed_out.
func exitCodeOf(err error, cmd *exec.Cmd) int {
	if err == nil {
		return 0
	}
	if state := cmd.ProcessState; state != nil {
		if code := state.ExitCode(); code >= 0 {
			return code
		}
	}
	return -1
}

// shellPath honors SHELL only when it is an absolute path — the container
// contract pins /bin/sh, and an inherited relative SHELL would make exec
// resolve against the workdir.
func shellPath() string {
	if shell := os.Getenv("SHELL"); shell != "" && shell[0] == '/' {
		return shell
	}
	return "/bin/sh"
}
