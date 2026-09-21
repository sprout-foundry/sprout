package console

import (
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"
)

// openRawPty returns a pty pair with the slave end in raw mode (the
// state probeBottomAnchored establishes before reading its reply) and
// registers cleanup. Raw mode matters: the DA2 reply contains no
// newline, so a canonical-mode slave would buffer it indefinitely and
// reads would starve.
func openRawPty(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	master, slave, err := pty.Open()
	if err != nil {
		t.Skipf("pty.Open failed: %v", err)
	}
	t.Cleanup(func() {
		_ = master.Close()
		_ = slave.Close()
	})
	raw, err := term.MakeRaw(int(slave.Fd()))
	if err != nil {
		t.Fatalf("MakeRaw on pty slave: %v", err)
	}
	t.Cleanup(func() { _ = term.Restore(int(slave.Fd()), raw) })
	return master, slave
}

// TestProbeBottomAnchoredDoesNotCloseStdin is the regression test for the
// DA2 probe closing the borrowed descriptor. The probe used to wrap the
// fd in os.NewFile, which registers a GC finalizer that closes the fd
// when the object becomes unreachable — so a probe on fd 0 (the REPL's
// stdin) detonated on the next GC cycle: close(0), and the next
// os.Stdin.Read failed with EBADF ("read /dev/stdin: bad file
// descriptor"), killing the interactive session. The fix reads the
// reply with raw poll+read syscalls that never close the fd. This test
// pins that contract: after probeBottomAnchored runs (and after forced
// GC cycles that would fire any finalizer), fd must still be readable.
func TestProbeBottomAnchoredDoesNotCloseStdin(t *testing.T) {
	master, slave := openRawPty(t)
	fd := int(slave.Fd())

	// Answer the DA2 request from the master end with a Termux-style
	// reply so the probe takes its happy path and consumes a reply.
	go func() {
		buf := make([]byte, 32)
		for {
			n, err := master.Read(buf)
			if err != nil || n == 0 {
				return
			}
			if strings.Contains(string(buf[:n]), "\033[>c") {
				_, _ = master.Write([]byte("\033[>41;320;0c"))
				return
			}
		}
	}()

	if got := probeBottomAnchored(uintptr(fd), slave); !got {
		t.Fatalf("probeBottomAnchored = false, want true for Termux DA2 reply")
	}

	// Force GC repeatedly so any finalizer registered on an *os.File
	// wrapping fd would run and close the descriptor.
	runtime.GC()
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	runtime.GC()

	// The fd must still be open — a TCGETS probe fails with EBADF on a
	// closed descriptor.
	if !term.IsTerminal(fd) {
		t.Fatal("probed fd is no longer a terminal — fd was closed by the probe path")
	}

	// A reply written now must still arrive: end-to-end proof the
	// descriptor is alive, not merely pollable.
	done := make(chan int, 1)
	go func() {
		_, _ = master.Write([]byte("x"))
		done <- 0
	}()
	chunk := make([]byte, 8)
	deadline := time.Now().Add(2 * time.Second)
	for {
		rn, rerr := slave.Read(chunk)
		if rn > 0 {
			break // descriptor still receives data
		}
		if rerr != nil {
			t.Fatalf("read on probed fd after GC: %v — fd was closed by the probe path", rerr)
		}
		if time.Now().After(deadline) {
			t.Fatal("no data arrived on probed fd after GC — descriptor dead")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestReadTTYReplyReturnsOnTerminator covers the reply reader's
// terminator semantics: it returns as soon as the terminator byte
// arrives rather than riding the full timeout, and its return value
// drives isTermuxDA2 correctly.
func TestReadTTYReplyReturnsOnTerminator(t *testing.T) {
	master, slave := openRawPty(t)

	go func() {
		time.Sleep(20 * time.Millisecond)
		_, _ = master.Write([]byte("\033[>41;320;0c"))
	}()

	const timeout = 2 * time.Second
	start := time.Now()
	reply := string(readTTYReply(int(slave.Fd()), timeout))
	elapsed := time.Since(start)

	if !isTermuxDA2(reply) {
		t.Fatalf("readTTYReply = %q, want a Termux DA2 reply", reply)
	}
	if elapsed >= timeout {
		t.Fatalf("readTTYReply blocked until timeout (%v) instead of returning at the terminator", elapsed)
	}
}

// TestReadTTYReplyTimesOutSilent covers the deadline path: with no
// reply ever written, the reader returns (not hangs) after the timeout
// with whatever it has — empty. Run back-to-back: poll can report the
// fd with no event bits at the timeout boundary (a rare kernel-timing
// race), and the read must stay bounded on every pass — a single pass
// would not shake that race out.
func TestReadTTYReplyTimesOutSilent(t *testing.T) {
	_, slave := openRawPty(t)

	const timeout = 60 * time.Millisecond
	for i := 0; i < 12; i++ {
		start := time.Now()
		reply := readTTYReply(int(slave.Fd()), timeout)
		elapsed := time.Since(start)

		if len(reply) != 0 {
			t.Fatalf("readTTYReply = %q on silent fd, want empty", reply)
		}
		if elapsed <= timeout/2 {
			t.Fatalf("readTTYReply returned after %v, want ~%v deadline wait (early return means the poll loop bailed)", elapsed, timeout)
		}
		if elapsed > 3*timeout {
			t.Fatalf("readTTYReply blocked %v past the deadline; the read must be bounded", elapsed)
		}
	}
}
