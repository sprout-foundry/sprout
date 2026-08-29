package console

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/creack/pty"
)

// withPtyStdin swaps os.Stdin for a PTY slave end for the duration of fn
// and restores it after. Returns a skip-friendly error when no PTY is
// available. The returned file is the master end — writes to it appear
// as keystrokes on stdin.
func withPtyStdin(t *testing.T) *os.File {
	t.Helper()
	master, slave, err := pty.Open()
	if err != nil {
		t.Skipf("pty.Open failed: %v", err)
	}
	t.Cleanup(func() {
		_ = master.Close()
		_ = slave.Close()
	})
	oldStdin := os.Stdin
	os.Stdin = slave
	t.Cleanup(func() { os.Stdin = oldStdin })
	return master
}

// TestWakeBeforeReadLine verifies that a Wake() set before ReadLine is
// honored: ReadLine returns ErrWakeupPending immediately instead of
// blocking on stdin. This is the "poller fired while the REPL was
// between ReadLine calls" window — a stale flag must not be swallowed.
func TestWakeBeforeReadLine(t *testing.T) {
	_ = withPtyStdin(t)
	ir := NewInputReader("> ")
	ir.Wake()

	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := ir.ReadLine()
		ch <- result{line, err}
	}()

	select {
	case res := <-ch:
		if !errors.Is(res.err, ErrWakeupPending) {
			t.Fatalf("ReadLine err = %v, want ErrWakeupPending", res.err)
		}
		if res.line != "" {
			t.Fatalf("ReadLine line = %q, want empty", res.line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ReadLine did not return despite pre-set Wake flag")
	}
}

// TestWakeDuringIdleReadLine verifies that Wake() interrupts an
// in-flight ReadLine: the loop's poll iteration observes the flag and
// returns ErrWakeupPending without waiting for a keystroke. This is
// the primary auto-resume path — the poller fires while the user idles
// at the prompt.
func TestWakeDuringIdleReadLine(t *testing.T) {
	master := withPtyStdin(t)
	ir := NewInputReader("> ")

	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := ir.ReadLine()
		ch <- result{line, err}
	}()

	// Give ReadLine a moment to enter its read loop, then wake it.
	// The mechanism is armed up front — Wake alone can't un-park a
	// Read that's already blocking.
	ir.ArmWakeup()
	time.Sleep(150 * time.Millisecond)
	ir.Wake()

	select {
	case res := <-ch:
		if !errors.Is(res.err, ErrWakeupPending) {
			t.Fatalf("ReadLine err = %v, want ErrWakeupPending", res.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("idle ReadLine did not return after Wake()")
	}
	_ = master
}

// TestLineBufferPreservedAfterWake verifies the REPL contract for
// ErrWakeupPending: any partial line the user had typed stays readable
// via LineBuffer so the caller can restore it with SetInitialContent.
func TestLineBufferPreservedAfterWake(t *testing.T) {
	master := withPtyStdin(t)
	ir := NewInputReader("> ")

	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := ir.ReadLine()
		ch <- result{line, err}
	}()

	// Type some text, then wake. Armed up front so the idle Read can
	// observe the flag.
	ir.ArmWakeup()
	_, _ = master.WriteString("half-typed")
	time.Sleep(150 * time.Millisecond)
	ir.Wake()

	select {
	case res := <-ch:
		if !errors.Is(res.err, ErrWakeupPending) {
			t.Fatalf("ReadLine err = %v, want ErrWakeupPending", res.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ReadLine did not return after Wake()")
	}
	if got := ir.LineBuffer(); got != "half-typed" {
		t.Fatalf("LineBuffer() = %q, want %q", got, "half-typed")
	}
}
