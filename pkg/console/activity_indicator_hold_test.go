package console

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func newHoldTestIndicator(t *testing.T) *ActivityIndicator {
	t.Helper()
	a := &ActivityIndicator{w: &nonTTYWriter{}, isTTY: true}
	RegisterGlobalIndicator(a)
	t.Cleanup(func() {
		a.Stop()
		RegisterGlobalIndicator(nil)
	})
	return a
}

// A tool starting under an open prompt must not draw the spinner over the
// question; the spinner comes back with that tool's label once the prompt
// is answered.
func TestIndicatorHold_StartDuringPromptIsDeferred(t *testing.T) {
	a := newHoldTestIndicator(t)
	a.Start("thinking")

	SuspendIndicatorResumable()
	require.False(t, a.IsActive(), "the prompt stops the spinner")

	a.Start("read_file")
	require.False(t, a.IsActive(), "a start under the prompt must not draw over it")

	ResumeIndicatorFromSuspend()
	msg, active := a.ActiveMessage()
	require.True(t, active)
	require.Equal(t, "read_file", msg, "resume shows the latest work, not the pre-prompt label")
}

func TestIndicatorHold_StopDuringPromptCancelsRestore(t *testing.T) {
	a := newHoldTestIndicator(t)
	a.Start("thinking")

	SuspendIndicatorResumable()
	a.Start("read_file")
	a.Stop() // the tool finished while the prompt was open
	ResumeIndicatorFromSuspend()

	require.False(t, a.IsActive(), "nothing is running, so nothing spins")
}

func TestIndicatorHold_NestedSuspendReleasesOnce(t *testing.T) {
	a := newHoldTestIndicator(t)
	a.Start("thinking")

	SuspendIndicatorResumable()
	SuspendIndicatorResumable()
	ResumeIndicatorFromSuspend()
	a.Start("grep")
	require.False(t, a.IsActive(), "the outer prompt is still open")

	ResumeIndicatorFromSuspend()
	msg, active := a.ActiveMessage()
	require.True(t, active)
	require.Equal(t, "grep", msg)
}
