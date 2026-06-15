//go:build (aix || linux || solaris || zos) && !js

package console

import (
	"testing"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

// TestGroundTruthTermios_RestoreActuallySetsTermios is the regression
// test for a bug where Restore() and EnsureCooked() used the TCGETS
// ioctl constant (READ / 0x5401) instead of TCSETS (WRITE / 0x5402).
// With the wrong constant, IoctlSetTermios is a no-op that reads the
// *current* termios into the buffer instead of writing the buffer to
// the terminal — so the terminal was never actually restored to cooked
// mode. This left VMIN=0 (set by enterSteerMode) in place, causing
// ask_user's bufio.Read to hit EOF immediately and report "stdin is
// not a TTY".
//
// This test reproduces the exact scenario: capture ground truth from a
// PTY, put the PTY into steer mode (ICANON off + VMIN=0), call
// Restore(), then verify ICANON is actually back on. With the TCGETS
// bug, Restore() would be a no-op and ICANON would remain off.
func TestGroundTruthTermios_RestoreActuallySetsTermios(t *testing.T) {
	// Create a PTY pair. The slave end is what we'd hand to a child
	// process; the master is for the test harness. We use the slave
	// fd as our terminal.
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatalf("pty.Open: %v", err)
	}
	defer func() {
		_ = master.Close()
		_ = slave.Close()
	}()

	fd := int(slave.Fd())

	// We need a GroundTruthTermios pointing at the slave fd. Capture
	// captures from os.Stdin.Fd(), so we construct one manually for
	// the test fd. CaptureGroundTruth returns nil on non-TTY, so we
	// verify the fd is a terminal first.
	origTermios, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		t.Fatalf("initial TCGETS: %v", err)
	}

	// Build a ground-truth snapshot manually (same logic as
	// CaptureGroundTruth but for our test fd).
	gt := &GroundTruthTermios{fd: fd, termios: *origTermios}
	normalizeCookedTermios(&gt.termios)

	// Sanity: ICANON should be on in the cooked ground truth.
	if gt.termios.Lflag&unix.ICANON == 0 {
		t.Fatal("ground truth should have ICANON on after normalizeCookedTermios")
	}

	// Simulate steer mode: ICANON off, VMIN=0 — exactly what
	// enterSteerMode does.
	steerTermios := *origTermios
	steerTermios.Lflag &^= unix.ICANON | unix.ECHO | unix.ISIG | unix.IEXTEN
	steerTermios.Cc[unix.VMIN] = 0
	steerTermios.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, &steerTermios); err != nil {
		t.Fatalf("failed to set steer mode: %v", err)
	}

	// Verify steer mode took effect (ICANON off, VMIN=0).
	current, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		t.Fatalf("post-steer TCGETS: %v", err)
	}
	if current.Lflag&unix.ICANON != 0 {
		t.Fatal("steer mode should have ICANON off")
	}
	if current.Cc[unix.VMIN] != 0 {
		t.Fatalf("steer mode should have VMIN=0, got %d", current.Cc[unix.VMIN])
	}

	// Now call Restore() — this is the method under test.
	if err := gt.Restore(); err != nil {
		t.Fatalf("Restore(): %v", err)
	}

	// Re-read the termios. With the TCGETS bug, Restore() was a no-op
	// and ICANON would still be off. With the fix (TCSETS), ICANON
	// should be back on.
	after, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		t.Fatalf("post-restore TCGETS: %v", err)
	}
	if after.Lflag&unix.ICANON == 0 {
		t.Error("Restore() did not restore ICANON — terminal still in raw mode. " +
			"Check that Restore() uses TCSETS (not TCGETS).")
	}
	if after.Cc[unix.VMIN] != 1 {
		t.Errorf("Restore() should set VMIN=1 (cooked), got VMIN=%d", after.Cc[unix.VMIN])
	}
}

// TestGroundTruthTermios_EnsureCookedActuallySetsTermios is the
// companion regression test for EnsureCooked(), which had the same
// TCGETS-instead-of-TCSETS bug as Restore().
func TestGroundTruthTermios_EnsureCookedActuallySetsTermios(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Fatalf("pty.Open: %v", err)
	}
	defer func() {
		_ = master.Close()
		_ = slave.Close()
	}()

	fd := int(slave.Fd())

	origTermios, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		t.Fatalf("initial TCGETS: %v", err)
	}

	gt := &GroundTruthTermios{fd: fd, termios: *origTermios}

	// Put the terminal into steer mode (ICANON off, VMIN=0).
	steerTermios := *origTermios
	steerTermios.Lflag &^= unix.ICANON | unix.ECHO | unix.ISIG | unix.IEXTEN
	steerTermios.Cc[unix.VMIN] = 0
	steerTermios.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, &steerTermios); err != nil {
		t.Fatalf("failed to set steer mode: %v", err)
	}

	// Call EnsureCooked — should unconditionally restore cooked mode.
	gt.EnsureCooked()

	after, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		t.Fatalf("post-EnsureCooked TCGETS: %v", err)
	}
	if after.Lflag&unix.ICANON == 0 {
		t.Error("EnsureCooked() did not restore ICANON — terminal still in raw mode. " +
			"Check that EnsureCooked() uses TCSETS (not TCGETS).")
	}
	if after.Cc[unix.VMIN] != 1 {
		t.Errorf("EnsureCooked() should set VMIN=1 (cooked), got VMIN=%d", after.Cc[unix.VMIN])
	}
}
