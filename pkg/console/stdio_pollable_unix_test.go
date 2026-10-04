//go:build unix && !js

package console

import (
	"bytes"
	"io"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/stretchr/testify/require"
	"golang.org/x/term"
)

// TestReopenPollable_WritesSurviveNonBlockingTerminal reproduces the input
// reader's state — O_NONBLOCK set on the terminal mid-session — and checks a
// burst larger than the tty buffer arrives whole through the re-opened file
// while the reader drains slowly. Through a plain blocking-mode *os.File the
// same write fails with EAGAIN and drops its tail.
func TestReopenPollable_WritesSurviveNonBlockingTerminal(t *testing.T) {
	master, slave, err := pty.Open()
	if err != nil {
		t.Skipf("no pty available: %v", err)
	}
	t.Cleanup(func() { _ = master.Close(); _ = slave.Close() })
	_, err = term.MakeRaw(int(slave.Fd()))
	require.NoError(t, err)

	// A dup shares the slave's open file description — and so its
	// O_NONBLOCK flag, as fds 0/1/2 of a terminal do — but gives the
	// re-opened file a descriptor of its own to close.
	dupFd, err := syscall.Dup(int(slave.Fd()))
	require.NoError(t, err)
	out, ok := reopenPollable(dupFd, "slave-dup")
	require.True(t, ok)
	t.Cleanup(func() { _ = out.Close() })
	require.NoError(t, syscall.SetNonblock(int(slave.Fd()), true))

	payload := bytes.Repeat([]byte("0123456789abcdef"), 16*1024)
	got := make(chan []byte, 1)
	go func() {
		var buf bytes.Buffer
		chunk := make([]byte, 512)
		for buf.Len() < len(payload) {
			time.Sleep(time.Millisecond)
			n, err := master.Read(chunk)
			buf.Write(chunk[:n])
			if err != nil && err != io.EOF {
				break
			}
		}
		got <- buf.Bytes()
	}()

	n, err := out.Write(payload)
	require.NoError(t, err)
	require.Equal(t, len(payload), n)
	select {
	case b := <-got:
		require.Equal(t, payload, b)
	case <-time.After(10 * time.Second):
		t.Fatal("reader did not receive the full payload")
	}
}
