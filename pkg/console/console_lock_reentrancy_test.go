package console

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestLockOutput_ReentrantPanics pins the re-entrancy detector. A second
// LockOutput on the goroutine that already holds outputMu can never
// succeed — it blocks forever — so it must fail loudly at the call site
// rather than wedge the REPL with no prompt.
func TestLockOutput_ReentrantPanics(t *testing.T) {
	LockOutput()
	defer UnlockOutput()

	require.PanicsWithValue(t,
		"console: re-entrant LockOutput — outputMu is not reentrant, call the *Locked variant",
		func() { LockOutput() },
	)
}

// TestLockOutput_ReentrantFromOtherGoroutineAcquires checks the detector is
// scoped to the owning goroutine: another goroutine still serializes
// normally once the holder releases.
func TestLockOutput_ReentrantFromOtherGoroutineAcquires(t *testing.T) {
	LockOutput()
	acquired := make(chan struct{})
	go func() {
		LockOutput()
		acquired <- struct{}{}
		UnlockOutput()
	}()
	UnlockOutput()
	<-acquired
}

// TestTryLockOutput_TracksHolder covers the TryLock path, which must record
// the holder so a later re-entrant LockOutput on the same goroutine is
// still caught.
func TestTryLockOutput_TracksHolder(t *testing.T) {
	require.True(t, TryLockOutput())
	defer UnlockOutput()
	require.Panics(t, func() { LockOutput() })
}

// TestPickerTeardown_RepaintsWithoutReentering drives the footer half of
// the picker teardown directly while holding outputMu, the way
// clearRendered calls it. A plain Refresh/draw inside would re-enter the
// lock and the detector would panic.
func TestPickerTeardown_RepaintsWithoutReentering(t *testing.T) {
	var buf bytes.Buffer
	f := NewStatusFooter(&buf, &stubSource{model: "test"})
	f.isTTY = true
	f.active = true
	f.sizeOverride = &terminalSizeOverride{cols: 80, rows: 24}
	RegisterGlobalStatusFooter(f)
	t.Cleanup(func() { RegisterGlobalStatusFooter(nil) })

	f.SetTurnActive(true)
	f.SetSteerLine("> half-typed steer")
	f.SetHintOverride("↑↓ select · enter resume")

	// The teardown's own writes are what is under test.
	buf.Reset()
	LockOutput()
	f.repaintAfterPickerLocked()
	UnlockOutput()

	// The last full chrome frame is what stays on screen; earlier frames
	// legitimately carried the steer text before it was cleared.
	out := buf.String()
	final := out
	if i := strings.LastIndex(out, "\033[23;1H"); i >= 0 {
		final = out[i:]
	}
	require.NotContains(t, final, "half-typed steer",
		"the steer text must not survive the teardown")
	require.NotContains(t, out, "↑↓ select",
		"the picker's hint override must be released on teardown")
	require.Contains(t, out, "\033[1;22r",
		"the footer re-asserts its idle scroll region (24 rows, 2 reserved)")
	require.Contains(t, final, "test", "the content row is repainted")
}

// TestClearSteerLine_Idempotent checks the shared clear path is a no-op
// when no steer panel is active, so a second call from Stop or a picker
// teardown cannot disturb the transcript.
func TestClearSteerLine_Idempotent(t *testing.T) {
	var buf strings.Builder
	f := newTeardownTestFooter(&buf, 24)
	f.ClearSteerLine()

	buf.Reset()
	f.ClearSteerLine()
	require.Empty(t, buf.String(), "second clear writes nothing")
}
