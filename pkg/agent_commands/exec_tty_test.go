//go:build unix && !js

package commands

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"github.com/sprout-foundry/sprout/pkg/clihooks"
	"github.com/stretchr/testify/require"
)

// withPTYStdio points os.Stdin/os.Stdout at a fresh pty and returns its
// master end, from which the test types and reads.
func withPTYStdio(t *testing.T) *os.File {
	t.Helper()
	master, slave, err := pty.Open()
	require.NoError(t, err)
	oldIn, oldOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = slave, slave
	t.Cleanup(func() {
		os.Stdin, os.Stdout = oldIn, oldOut
		_ = slave.Close()
		_ = master.Close()
	})
	return master
}

// The regression: `!` ran children with /dev/null for stdin, so anything
// that asked a question (a password, a passphrase, a y/N) got EOF. On a
// terminal the child now reads what the user types.
func TestRunOnTerminal_ChildReadsTheUsersAnswer(t *testing.T) {
	master := withPTYStdio(t)

	var out bytes.Buffer
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buf := make([]byte, 1024)
		for {
			n, err := master.Read(buf)
			out.Write(buf[:n])
			if strings.Contains(out.String(), "got:hunter2") || err != nil {
				return
			}
		}
	}()

	type result struct {
		code int
		err  error
	}
	done := make(chan result, 1)
	go func() {
		code, err := runOnTerminal(`printf 'Password: '; read -r pw; echo "got:$pw"`, t.TempDir())
		done <- result{code, err}
	}()
	_, err := io.WriteString(master, "hunter2\n")
	require.NoError(t, err)

	select {
	case r := <-done:
		require.NoError(t, r.err)
		require.Equal(t, 0, r.code)
	case <-time.After(5 * time.Second):
		t.Fatal("the child never finished: its stdin is not the terminal")
	}
	select {
	case <-readDone:
	case <-time.After(5 * time.Second):
	}
	require.Contains(t, out.String(), "got:hunter2")
}

func TestRunOnTerminal_ReportsExitStatus(t *testing.T) {
	withPTYStdio(t)
	code, err := runOnTerminal("exit 3", t.TempDir())
	require.NoError(t, err)
	require.Equal(t, 3, code)
}

// While the child holds the terminal, Ctrl+C is the child's: sprout's
// SIGINT handler consults this to stand aside.
func TestRunOnTerminal_MarksForegroundChild(t *testing.T) {
	withPTYStdio(t)
	require.False(t, clihooks.ForegroundChildActive())
	go func() {
		_, _ = runOnTerminal("sleep 0.3", t.TempDir())
	}()
	deadline := time.Now().Add(2 * time.Second)
	for !clihooks.ForegroundChildActive() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	require.True(t, clihooks.ForegroundChildActive(), "marked while the child runs")
	for clihooks.ForegroundChildActive() && time.Now().Before(deadline.Add(2*time.Second)) {
		time.Sleep(10 * time.Millisecond)
	}
	require.False(t, clihooks.ForegroundChildActive(), "cleared when it exits")
}
