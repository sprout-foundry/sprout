//go:build unix && !js

package console

import (
	"io"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func readAllAfterClose(t *testing.T, r, w *os.File) string {
	t.Helper()
	require.NoError(t, w.Close())
	out, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(out)
}

// A background line that arrives while another writer holds the output lock
// waits for it instead of writing around it: the holder's output and the
// line come out in order, and the line goes through the transcript writer.
func TestPrintBackground_WaitsForBusyWriter(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })

	LockOutput()
	released := make(chan struct{})
	go func() {
		time.Sleep(50 * time.Millisecond)
		_, _ = w.WriteString("holder\n")
		UnlockOutput()
		close(released)
	}()
	printBackground("[coder:t1] line", w)
	<-released

	require.Equal(t, "holder\n[coder:t1] line\n", readAllAfterClose(t, r, w))
}

// A holder that never lets go (possibly this goroutine's own caller) must not
// wedge the writer: after the bound it writes anyway.
func TestPrintBackground_BoundedWhenLockStaysHeld(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })

	LockOutput()
	defer UnlockOutput()
	start := time.Now()
	printBackground("still printed", w)
	require.Less(t, time.Since(start), 2*time.Second)
	require.GreaterOrEqual(t, time.Since(start), backgroundLockWait)
	require.Equal(t, "still printed\n", readAllAfterClose(t, r, w))
}
