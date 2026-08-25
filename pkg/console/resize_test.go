package console

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRegisterResizeSubscriber(t *testing.T) {
	// Register a subscriber and verify it receives the width.
	var (
		mu    sync.Mutex
		gotW  int
		calls int
	)

	unsub := RegisterResizeSubscriber(func(w int) {
		mu.Lock()
		defer mu.Unlock()
		gotW = w
		calls++
	})
	defer unsub()

	// notifyResizeSubscribers reads the live terminal width. In a test
	// environment (no TTY), it falls back to 80.
	notifyResizeSubscribers()

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 1, calls, "subscriber should be called once")
	require.Equal(t, 80, gotW, "should receive fallback width 80 in non-TTY test")
}

func TestResizeSubscriberDeregistration(t *testing.T) {
	var calls int
	var mu sync.Mutex

	unsub := RegisterResizeSubscriber(func(w int) {
		mu.Lock()
		defer mu.Unlock()
		calls++
	})

	notifyResizeSubscribers()
	unsub()
	notifyResizeSubscribers()

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 1, calls, "subscriber should only be called once (before deregistration)")
}

func TestMultipleResizeSubscribers(t *testing.T) {
	var (
		mu     sync.Mutex
		calls1 int
		calls2 int
	)

	unsub1 := RegisterResizeSubscriber(func(w int) {
		mu.Lock()
		defer mu.Unlock()
		calls1++
	})
	defer unsub1()

	unsub2 := RegisterResizeSubscriber(func(w int) {
		mu.Lock()
		defer mu.Unlock()
		calls2++
	})
	defer unsub2()

	notifyResizeSubscribers()

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 1, calls1, "subscriber 1 should be called")
	require.Equal(t, 1, calls2, "subscriber 2 should be called")
}

func TestRendererSetTerminalWidth(t *testing.T) {
	r := NewAssistantTurnRenderer(120, NewMarkdownFormatter(true, true))

	// Initial width
	require.Equal(t, 120, r.terminalWidth, "initial width should be 120")
	require.Equal(t, 120, r.formatter.width, "formatter width should be 120")

	// Update width
	r.SetTerminalWidth(80)
	require.Equal(t, 80, r.terminalWidth, "renderer width should update to 80")
	require.Equal(t, 80, r.formatter.width, "formatter width should update to 80")

	// Zero or negative width falls back to 80
	r.SetTerminalWidth(0)
	require.Equal(t, 80, r.terminalWidth, "zero width should fall back to 80")
	require.Equal(t, 80, r.formatter.width, "formatter width should fall back to 80")

	r.SetTerminalWidth(-1)
	require.Equal(t, 80, r.terminalWidth, "negative width should fall back to 80")
	require.Equal(t, 80, r.formatter.width, "formatter width should fall back to 80")
}

func TestRendererSetTerminalWidthConcurrent(t *testing.T) {
	r := NewAssistantTurnRenderer(80, NewMarkdownFormatter(true, true))

	// Concurrently update width and read — should not race
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			r.SetTerminalWidth(60 + n)
		}(i)
	}
	wg.Wait()

	// Final width should be one of the set values (between 60-79)
	require.GreaterOrEqual(t, r.terminalWidth, 60, "width should be in the set range")
	require.LessOrEqual(t, r.terminalWidth, 79, "width should be in the set range")
}

func TestStartResizePollerNoOpOnUnix(t *testing.T) {
	// On Unix, startResizeWatcher is a no-op (SIGWINCH is handled by the
	// footer's watchResize goroutine). Verify it returns immediately.
	done := make(chan struct{})
	go func() {
		stop := startResizePoller(func() {})
		stop()
		close(done)
	}()

	select {
	case <-done:
		// Good — returned immediately
	case <-time.After(2 * time.Second):
		t.Fatal("startResizePoller should return immediately on Unix (no-op)")
	}
}
