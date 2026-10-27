//go:build unix && !js

package console

// Terminal re-sync regression tests ("stuck in a weird state" after a `!`
// shell command or a timed-out security prompt).
//
// They reproduce the BROKEN state an out-of-band TTY interaction leaves
// behind (raw termios, alternate screen, hidden cursor, desynced scroll
// region) and assert ResyncTerminal restores every piece.

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"
)

// resyncHarness wires stdin/stdout/stderr to a real pty and captures
// everything written to it in a background pump.
type resyncHarness struct {
	t      *testing.T
	master *os.File
	slave  *os.File

	mu  sync.Mutex
	buf []byte
}

func testPTY(t *testing.T, rows, cols uint16) *resyncHarness {
	t.Helper()
	master, slave, err := pty.Open()
	if err != nil {
		t.Skipf("no pty: %v", err)
	}
	_ = pty.Setsize(master, &pty.Winsize{Rows: rows, Cols: cols})

	h := &resyncHarness{t: t, master: master, slave: slave}
	go func() {
		buf := make([]byte, 65536)
		for {
			n, rerr := master.Read(buf)
			if n > 0 {
				h.mu.Lock()
				h.buf = append(h.buf, buf[:n]...)
				h.mu.Unlock()
			}
			if rerr != nil {
				return
			}
		}
	}()

	oldStdin, oldStdout, oldStderr := os.Stdin, os.Stdout, os.Stderr
	os.Stdin = slave
	os.Stdout = slave
	os.Stderr = slave
	t.Cleanup(func() {
		os.Stdin, os.Stdout, os.Stderr = oldStdin, oldStdout, oldStderr
		// Closing the pty ends the pump's blocked Read. (Order matters:
		// restore os.* before closing so no writer races the close.)
		_ = master.Close()
		_ = slave.Close()
	})

	// Neutralize the DA2 terminal-flavor probe: against a bare pty it
	// would wait its full 250ms deadline (and cache a flavor) on
	// footer.Start(). Bypass it entirely for a fast, deterministic test.
	oldProbe := probeOverride
	probeOverride = func() bool { return false }
	t.Cleanup(func() { probeOverride = oldProbe })

	return h
}

// stream gives the pty line discipline a beat to flush the bytes written to
// the slave, then returns the captured bytes. It does NOT close the master
// (closing a pty fd does not unblock the pump's blocked Read, which would
// hang the wait) — the cleanup closes both ends.
func (h *resyncHarness) stream() string {
	time.Sleep(200 * time.Millisecond)
	h.mu.Lock()
	defer h.mu.Unlock()
	return string(h.buf)
}

type resyncFooterSource struct{}

func (resyncFooterSource) Model() string             { return "m" }
func (resyncFooterSource) ContextTokens() (int, int) { return 1, 200000 }
func (resyncFooterSource) TotalCost() float64        { return 0 }
func (resyncFooterSource) WorkingDir() string        { return "." }

// TestResyncTerminalRestoresCookedTermios reproduces the "can't type"
// symptom: an out-of-band child leaves stdin in RAW mode (ICANON off), so
// the user's lines never reach the next cooked ReadLine. ResyncTerminal
// must restore the startup (ground-truth) cooked snapshot.
func TestResyncTerminalRestoresCookedTermios(t *testing.T) {
	testPTY(t, 40, 100)
	fd := int(os.Stdin.Fd())

	gt := CaptureGroundTruth()
	if gt == nil {
		t.Fatal("CaptureGroundTruth returned nil on a pty")
	}
	RegisterGlobalGroundTruth(gt)
	t.Cleanup(func() { RegisterGlobalGroundTruth(nil) })

	// Damage: put the TTY into raw mode, as a TUI child that failed to
	// restore would.
	if _, err := term.MakeRaw(fd); err != nil {
		t.Fatalf("MakeRaw: %v", err)
	}
	// Precondition: the TTY is now NOT in cooked (sane) mode.
	if gt.IsTerminalSane() {
		t.Fatalf("precondition failed: raw mode did not break cooked state")
	}

	ResyncTerminal()

	if !gt.IsTerminalSane() {
		t.Error("ResyncTerminal did not restore cooked mode: terminal still not sane (ICANON off)")
	}
}

// TestResyncTerminalReassertsRegionAndCursor reproduces the "garbled +
// invisible cursor + footer broken" symptoms: an out-of-band interaction
// left the terminal in the ALTERNATE screen with the cursor hidden and the
// footer's scroll region desynced. ResyncTerminal must exit the alt
// screen, re-show the cursor, and re-assert the footer's region.
func TestResyncTerminalReassertsRegionAndCursor(t *testing.T) {
	h := testPTY(t, 40, 100)

	footer := NewStatusFooter(nil, resyncFooterSource{})
	footer.Start()
	t.Cleanup(footer.Stop)
	RegisterGlobalStatusFooter(footer)
	t.Cleanup(func() { RegisterGlobalStatusFooter(nil) })
	footer.Refresh()

	// Damage: enter the alternate screen and hide the cursor — exactly
	// what a pager/editor/TUI child does on takeover.
	_, _ = os.Stdout.Write([]byte("\x1b[?1049h\x1b[?25l"))

	ResyncTerminal()

	stream := h.stream()
	if !strings.Contains(stream, "\x1b[?1049l") {
		t.Error("resync stream does not exit the alternate screen (?1049l)")
	}
	if !strings.Contains(stream, "\x1b[?25h") {
		t.Error("resync stream does not re-show the cursor (?25h)")
	}
	// The footer's Refresh after the resync re-asserts the scroll region:
	// DECSTBM `\x1b[1;<N>r` with N = rows - reserved = 40 - 2 = 38.
	if !strings.Contains(stream, "\x1b[1;38r") {
		t.Error("resync stream does not re-assert the footer scroll region (want \\033[1;38r)")
	}
}

// TestResyncTerminalEmitsNothingOnNonTTY guards the pipe/CI path: with
// stdout a pipe (not a TTY) the escape sequences must NOT be emitted —
// they would pollute a captured stream or CI log.
func TestResyncTerminalEmitsNothingOnNonTTY(t *testing.T) {
	oldStdout := os.Stdout
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	os.Stdout = pw
	t.Cleanup(func() {
		os.Stdout = oldStdout
		_ = pr.Close()
		_ = pw.Close()
	})

	RegisterGlobalGroundTruth(nil)
	ResyncTerminal() // must not panic; must emit nothing to a pipe

	// Non-blocking check: a bare pr.Read would block forever on the
	// (empty) pipe, so poll via a goroutine + timeout window instead.
	emitted := make(chan int, 1)
	go func() {
		buf := make([]byte, 4096)
		n, _ := pr.Read(buf)
		emitted <- n
	}()
	select {
	case got := <-emitted:
		if got > 0 {
			t.Errorf("ResyncTerminal emitted %d bytes to a non-TTY stdout", got)
		}
	case <-time.After(300 * time.Millisecond):
		// nothing written within the window — correct for a non-TTY stdout
	}
}
