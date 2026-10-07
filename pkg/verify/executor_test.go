package verify

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shAvailable reports whether the default executor's shell exists on
// this platform; the real-execution tests skip rather than fail where it
// does not (non-UNIX dev machines).
func shAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh available on this platform")
	}
}

// TestShellExecutorRealCommands proves the real execution path (no
// fakes): the default executor runs a real shell command, reports
// pass/fail from the exit code, and captures combined output.
func TestShellExecutorRealCommands(t *testing.T) {
	shAvailable(t)
	e := &ShellExecutor{}
	root := t.TempDir()

	ok, err := e.Run(context.Background(), root, "echo hello")
	require.NoError(t, err)
	assert.True(t, ok.Passed)
	assert.Equal(t, "hello\n", ok.Output)

	bad, err := e.Run(context.Background(), root, "echo boom 1>&2; exit 3")
	require.NoError(t, err, "a non-zero exit is an outcome, not an error")
	assert.False(t, bad.Passed)
	assert.Equal(t, "boom\n", bad.Output, "combined stderr must be captured")
	assert.Empty(t, bad.Reason, "a normal failure has no reason")
}

// TestShellExecutorTimeout proves the bounded-timeout path: a slow
// command is stopped by the context and reported as a failed outcome
// with a reason, with the run actually bounded.
func TestShellExecutorTimeout(t *testing.T) {
	shAvailable(t)
	e := &ShellExecutor{}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	out, err := e.Run(ctx, t.TempDir(), "sleep 2")
	elapsed := time.Since(start)

	require.NoError(t, err, "a timeout is an outcome with a reason, not an error")
	assert.False(t, out.Passed)
	assert.Contains(t, out.Reason, "timeout or cancellation")
	assert.Less(t, elapsed, time.Second, "the timeout must actually bound the run (took %s)", elapsed)
}

// TestShellExecutorLaunchError proves the setup-failure path: a
// nonexistent shell is an error, not an outcome.
func TestShellExecutorLaunchError(t *testing.T) {
	e := &ShellExecutor{Shell: "/nonexistent/sprout-verify-shell"}

	out, err := e.Run(context.Background(), t.TempDir(), "echo hi")
	require.Error(t, err, "an unlaunchable shell must surface as an error")
	assert.False(t, out.Passed)
}

// TestRunRealBaseline proves the full real path end to end: a runner
// with the default shell executor executes trivial real commands and
// reports them in the structured result.
func TestRunRealBaseline(t *testing.T) {
	shAvailable(t)
	root := t.TempDir()
	writeManifestFile(t, root, `{
	  "starter": {"id": "trivial", "version": "0.1.0"},
	  "build": "echo build-ok",
	  "test": "echo test-ok"
	}`)

	r := New()
	r.Timeout = 30 * time.Second

	res, err := r.Run(context.Background(), root)
	require.NoError(t, err)

	assert.True(t, res.Baseline)
	require.Len(t, res.Checks, 2)
	assert.True(t, res.Checks[0].Passed)
	assert.Contains(t, res.Checks[0].Excerpt, "build-ok")
	assert.True(t, res.Checks[1].Passed)
	assert.Contains(t, res.Checks[1].Excerpt, "test-ok")
	assert.True(t, res.Passed())
	assert.NotZero(t, res.Checks[0].Duration, "the check duration must be recorded")
}

// TestRunRealTimeoutPropagatesReason proves the timeout path through the
// full runner flow: a slow real command is stopped by the runner's
// per-check timeout, the check fails with the timeout reason, and the
// run is reported as failed (the verification evidence).
func TestRunRealTimeoutPropagatesReason(t *testing.T) {
	shAvailable(t)
	root := t.TempDir()
	writeManifestFile(t, root, `{
	  "starter": {"id": "trivial", "version": "0.1.0"},
	  "build": "echo build-ok",
	  "test": "sleep 2 && echo test-ok"
	}`)

	r := New()
	r.Timeout = 100 * time.Millisecond

	res, err := r.Run(context.Background(), root)
	require.NoError(t, err)

	require.Len(t, res.Checks, 2)
	assert.True(t, res.Checks[0].Passed, "the fast build check must pass")
	assert.False(t, res.Checks[1].Passed, "the slow test check must time out")
	assert.Contains(t, res.Checks[1].Reason, "timeout or cancellation")
	assert.True(t, res.Failed(), "a timed-out check must fail the run")
}
