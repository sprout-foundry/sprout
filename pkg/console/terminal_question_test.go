package console

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTerminalQuestion_SecondWaitsForFirst(t *testing.T) {
	release, err := AcquireTerminalQuestion(context.Background())
	require.NoError(t, err)

	got := make(chan struct{})
	go func() {
		r, err := AcquireTerminalQuestion(context.Background())
		require.NoError(t, err)
		close(got)
		r()
	}()
	select {
	case <-got:
		t.Fatal("a second question took the terminal while the first was open")
	case <-time.After(50 * time.Millisecond):
	}
	release()
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("the waiting question never got the terminal")
	}
}

func TestTerminalQuestion_CancelledWaitGivesUp(t *testing.T) {
	release, err := AcquireTerminalQuestion(context.Background())
	require.NoError(t, err)
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan error, 1)
	go func() {
		_, err := AcquireTerminalQuestion(ctx)
		done <- err
	}()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("a cancelled wait did not return")
	}
}

func TestTerminalQuestion_ReentrantOnTheHoldingGoroutine(t *testing.T) {
	outer, err := AcquireTerminalQuestion(context.Background())
	require.NoError(t, err)
	inner, err := AcquireTerminalQuestion(context.Background())
	require.NoError(t, err, "a prompt raised inside another must not deadlock")
	inner()
	outer()

	again, err := AcquireTerminalQuestion(context.Background())
	require.NoError(t, err, "fully released after the outer release")
	again()
}
