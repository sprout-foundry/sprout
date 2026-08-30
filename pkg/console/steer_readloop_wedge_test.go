//go:build unix && !js

package console

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/term"
)

// Regression for "[steer] Stop() timed out waiting for readLoop": a
// readLoop parked in a blocking Read wedges forever when something
// restores cooked mode (VMIN=1) under it — exactly what a prompt's
// exitSteerMode or a racing PauseSteer does. The poll-gated read bounds
// every wait to 10ms regardless of termios, so Stop() must observe the
// loop exit well inside its 2s timeout even from cooked mode.
//
// Runs only on a real TTY (CI/CI-less environments without a pty skip).
func TestSteerReadLoop_ExitsPromptlyFromCookedMode(t *testing.T) {
	fd := int(os.Stdin.Fd())
	if !isTerminal(fd) {
		t.Skip("stdin is not a TTY; needs a pty to exercise termios")
	}

	r := &SteerInputReader{fd: fd, isTTY: true}
	stopCh := make(chan struct{})
	doneCh := make(chan struct{})

	go r.readLoop(stopCh, doneCh)

	// Flip stdin to cooked mode WHILE the loop is running — simulates
	// the racing exitSteerMode/PauseSteer that used to wedge the loop.
	saved, err := enterSteerMode(fd)
	require.NoError(t, err)
	defer func() { _ = exitSteerMode(fd, saved) }()

	time.Sleep(20 * time.Millisecond) // let the loop park on a read

	close(stopCh)
	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("readLoop did not exit within Stop()'s 2s budget after termios was flipped to cooked mode")
	}
}

func isTerminal(fd int) bool {
	return term.IsTerminal(fd)
}
