package clihooks

import (
	"sync"
	"testing"
)

func TestSuspendIndicator_NoOpWithoutHook(t *testing.T) {
	// Reset any global state from previous tests.
	SetSuspendIndicator(nil)
	// Must not panic when nothing is registered.
	SuspendIndicator()
}

func TestSuspendIndicator_CallsRegisteredHook(t *testing.T) {
	t.Cleanup(func() { SetSuspendIndicator(nil) })

	called := 0
	SetSuspendIndicator(func() { called++ })

	SuspendIndicator()
	SuspendIndicator()

	if called != 2 {
		t.Errorf("expected hook to be called twice, got %d", called)
	}
}

func TestSetSuspendIndicator_NilClearsHook(t *testing.T) {
	t.Cleanup(func() { SetSuspendIndicator(nil) })

	called := 0
	SetSuspendIndicator(func() { called++ })
	SuspendIndicator()
	SetSuspendIndicator(nil)
	SuspendIndicator()

	if called != 1 {
		t.Errorf("expected hook to fire once before being cleared, got %d", called)
	}
}

func TestSetSuspendIndicator_ReplacesPriorHook(t *testing.T) {
	t.Cleanup(func() { SetSuspendIndicator(nil) })

	var firstCount, secondCount int
	SetSuspendIndicator(func() { firstCount++ })
	SetSuspendIndicator(func() { secondCount++ })
	SuspendIndicator()

	if firstCount != 0 {
		t.Errorf("first hook should be replaced, got %d calls", firstCount)
	}
	if secondCount != 1 {
		t.Errorf("second hook should be called once, got %d", secondCount)
	}
}

func TestSetSuspendIndicator_ConcurrentSafe(t *testing.T) {
	t.Cleanup(func() { SetSuspendIndicator(nil) })

	var calls int64
	var mu sync.Mutex
	SetSuspendIndicator(func() {
		mu.Lock()
		calls++
		mu.Unlock()
	})

	const goroutines = 50
	const callsEach = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < callsEach; j++ {
				SuspendIndicator()
			}
		}()
	}
	wg.Wait()

	mu.Lock()
	got := calls
	mu.Unlock()
	if got != int64(goroutines*callsEach) {
		t.Errorf("expected %d calls, got %d", goroutines*callsEach, got)
	}
}
