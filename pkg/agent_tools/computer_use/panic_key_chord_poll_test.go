package computer_use

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeKeyReader struct {
	mu     sync.Mutex
	down   map[int]bool
	closed atomic.Bool
}

var fakeCodes = map[string][]int{"ctrl": {1, 2}, "shift": {3}, "escape": {4}}

func (r *fakeKeyReader) resolve(token string) ([]int, bool) {
	codes, ok := fakeCodes[canonicalChordKey(token)]
	return codes, ok
}

func (r *fakeKeyReader) held() (func(int) bool, error) {
	r.mu.Lock()
	snapshot := make(map[int]bool, len(r.down))
	for k, v := range r.down {
		snapshot[k] = v
	}
	r.mu.Unlock()
	return func(code int) bool { return snapshot[code] }, nil
}

func (r *fakeKeyReader) close() { r.closed.Store(true) }

func (r *fakeKeyReader) press(codes ...int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.down = map[int]bool{}
	for _, c := range codes {
		r.down[c] = true
	}
}

func startFakeWatcher(t *testing.T, keys []string) (*fakeKeyReader, *atomic.Int32, *pollingChordWatcher) {
	t.Helper()
	reader := &fakeKeyReader{}
	var fired atomic.Int32
	w := &pollingChordWatcher{
		keys: keys,
		open: func() (keyReader, error) { return reader, nil },
		fire: func() error { fired.Add(1); return nil },
	}
	if err := w.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(w.Stop)
	return reader, &fired, w
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met within 2s")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func settle() { time.Sleep(4 * chordPollInterval) }

func TestPollingChordWatcher_FiresOncePerPress(t *testing.T) {
	reader, fired, _ := startFakeWatcher(t, []string{"ctrl", "shift", "escape"})

	reader.press(1, 3)
	settle()
	if n := fired.Load(); n != 0 {
		t.Fatalf("partial chord fired %d times", n)
	}

	reader.press(2, 3, 4)
	waitFor(t, func() bool { return fired.Load() == 1 })
	settle()
	if n := fired.Load(); n != 1 {
		t.Fatalf("held chord fired %d times, want 1", n)
	}

	reader.press()
	settle()
	reader.press(1, 3, 4)
	waitFor(t, func() bool { return fired.Load() == 2 })
}

func TestPollingChordWatcher_OutlivesStartContext(t *testing.T) {
	reader := &fakeKeyReader{}
	var fired atomic.Int32
	w := &pollingChordWatcher{
		keys: []string{"escape"},
		open: func() (keyReader, error) { return reader, nil },
		fire: func() error { fired.Add(1); return nil },
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	cancel()
	t.Cleanup(w.Stop)

	reader.press(4)
	waitFor(t, func() bool { return fired.Load() == 1 })
}

func TestPollingChordWatcher_StopClosesReader(t *testing.T) {
	reader, fired, w := startFakeWatcher(t, []string{"ctrl", "escape"})
	if got := ArmedPanicChord(); got != "ctrl+escape" {
		t.Fatalf("ArmedPanicChord = %q while running", got)
	}
	w.Stop()
	if got := ArmedPanicChord(); got != "" {
		t.Fatalf("ArmedPanicChord = %q after Stop", got)
	}
	if !reader.closed.Load() {
		t.Fatal("reader not closed after Stop")
	}
	reader.press(2, 4)
	settle()
	if n := fired.Load(); n != 0 {
		t.Fatalf("stopped watcher fired %d times", n)
	}
	w.Stop()
}

func TestPollingChordWatcher_StartErrors(t *testing.T) {
	openErr := errors.New("no permission")
	w := &pollingChordWatcher{keys: []string{"escape"}, open: func() (keyReader, error) { return nil, openErr }}
	if err := w.Start(context.Background()); !errors.Is(err, openErr) {
		t.Fatalf("Start = %v, want %v", err, openErr)
	}

	reader := &fakeKeyReader{}
	w = &pollingChordWatcher{keys: []string{"ctrl", "hyper"}, open: func() (keyReader, error) { return reader, nil }}
	if err := w.Start(context.Background()); err == nil {
		t.Fatal("Start accepted an unknown key")
	}
	if !reader.closed.Load() {
		t.Fatal("reader left open after an unknown key")
	}
}

func TestPollingChordWatcher_AliasesResolve(t *testing.T) {
	reader, fired, _ := startFakeWatcher(t, []string{"control", "esc"})
	reader.press(2, 4)
	waitFor(t, func() bool { return fired.Load() == 1 })
}
