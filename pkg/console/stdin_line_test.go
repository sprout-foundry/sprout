//go:build unix && !js

package console

import (
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newLinePipe(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = r.Close()
		_ = w.Close()
	})
	return r, w
}

func TestReadLineCtx_SingleEnterSubmits(t *testing.T) {
	r, w := newLinePipe(t)
	_, err := w.WriteString("yes please\n")
	require.NoError(t, err)

	got, err := readLineCtx(context.Background(), int(r.Fd()), r)
	require.NoError(t, err)
	require.Equal(t, "yes please", got)
}

func TestReadLineCtx_PasteBurstIsOneAnswer(t *testing.T) {
	r, w := newLinePipe(t)
	_, err := w.WriteString("line one\nline two\n\nline four\n")
	require.NoError(t, err)

	got, err := readLineCtx(context.Background(), int(r.Fd()), r)
	require.NoError(t, err)
	require.Equal(t, "line one\nline two\n\nline four", got,
		"a paste's later lines belong to the answer, not to the next reader")
}

func TestReadLineCtx_LaterTypingIsNotConsumed(t *testing.T) {
	r, w := newLinePipe(t)
	_, err := w.WriteString("first\n")
	require.NoError(t, err)

	got, err := readLineCtx(context.Background(), int(r.Fd()), r)
	require.NoError(t, err)
	require.Equal(t, "first", got)

	// Input that arrives after the burst window stays on the stream.
	_, err = w.WriteString("next\n")
	require.NoError(t, err)
	got, err = readLineCtx(context.Background(), int(r.Fd()), r)
	require.NoError(t, err)
	require.Equal(t, "next", got)
}

// Cancelling must return promptly and leave nothing reading the stream: the
// input written afterwards is all there for the next reader.
func TestReadLineCtx_CancelLeavesNoReader(t *testing.T) {
	r, w := newLinePipe(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := readLineCtx(ctx, int(r.Fd()), r)
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled read did not return")
	}

	_, err := w.WriteString("for the next reader\n")
	require.NoError(t, err)
	got, err := readLineCtx(context.Background(), int(r.Fd()), r)
	require.NoError(t, err)
	require.Equal(t, "for the next reader", got)
}

func TestReadLineCtx_EOFBeforeInput(t *testing.T) {
	r, w := newLinePipe(t)
	require.NoError(t, w.Close())
	_, err := readLineCtx(context.Background(), int(r.Fd()), r)
	require.ErrorIs(t, err, io.EOF)
}
