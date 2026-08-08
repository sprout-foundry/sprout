//go:build !js

package daemon

import (
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// TestLifecycleIdleReap
//
// Add→Remove (count hits 0) → timer fires → StopFunc invoked exactly once.
// ---------------------------------------------------------------------------

func TestLifecycleIdleReap(t *testing.T) {

	var stopped int64
	lc := NewDaemonLifecycle(50*time.Millisecond, func() error {
		atomic.AddInt64(&stopped, 1)
		return nil
	})

	lc.Add()    // count = 1
	lc.Remove() // count = 0, timer starts

	// Wait for timer to fire.
	select {
	case <-time.After(2 * time.Second):
		t.Fatal("StopFunc never called after idle timeout")
	case <-waitUntilStopped(t, &stopped):
	}

	if v := atomic.LoadInt64(&stopped); v != 1 {
		t.Errorf("StopFunc called %d times; want exactly 1", v)
	}
}

// ---------------------------------------------------------------------------
// TestLifecycleAddDuringTeardown
//
// Add→Remove→Add (before timer fires) → StopFunc must NOT be called.
// ---------------------------------------------------------------------------

func TestLifecycleAddDuringTeardown(t *testing.T) {

	var stopped int64
	delay := 100 * time.Millisecond
	lc := NewDaemonLifecycle(delay, func() error {
		atomic.AddInt64(&stopped, 1)
		return nil
	})

	lc.Add()    // count = 1
	lc.Remove() // count = 0, timer starts

	// Immediately re-add before the delay elapses.
	time.Sleep(20 * time.Millisecond)
	lc.Add() // count = 1, timer should be cancelled

	// Wait several multiples of the delay.
	time.Sleep(5 * delay)

	if v := atomic.LoadInt64(&stopped); v != 0 {
		t.Errorf("StopFunc called %d times; want 0 (timer was cancelled by re-Add)", v)
	}
}

// ---------------------------------------------------------------------------
// TestLifecycleCountTracking
//
// Verify Count() returns correct values across Add/Remove sequences.
// ---------------------------------------------------------------------------

func TestLifecycleCountTracking(t *testing.T) {

	lc := NewDaemonLifecycle(time.Hour, nil) // long delay so timer never fires during test

	if got := lc.Add(); got != 1 {
		t.Errorf("Add() returned %d; want 1", got)
	}
	if got := lc.Add(); got != 2 {
		t.Errorf("Add() returned %d; want 2", got)
	}
	if got := lc.Add(); got != 3 {
		t.Errorf("Add() returned %d; want 3", got)
	}
	if got := lc.Count(); got != 3 {
		t.Errorf("Count() = %d; want 3", got)
	}

	if got := lc.Remove(); got != 2 {
		t.Errorf("Remove() returned %d; want 2", got)
	}
	if got := lc.Remove(); got != 1 {
		t.Errorf("Remove() returned %d; want 1", got)
	}
	if got := lc.Remove(); got != 0 {
		t.Errorf("Remove() returned %d; want 0", got)
	}

	// Extra Remove when already at 0 should not go negative.
	if got := lc.Remove(); got != 0 {
		t.Errorf("Remove() at 0 returned %d; want 0", got)
	}
}

// ---------------------------------------------------------------------------
// TestLifecycleCloseStopsTimer
//
// Close() while timer is pending → StopFunc must never be invoked.
// ---------------------------------------------------------------------------

func TestLifecycleCloseStopsTimer(t *testing.T) {

	var stopped int64
	lc := NewDaemonLifecycle(50*time.Millisecond, func() error {
		atomic.AddInt64(&stopped, 1)
		return nil
	})

	lc.Add()    // count = 1
	lc.Remove() // count = 0, timer starts

	if !lc.TimerActive() {
		t.Fatal("Timer should be active after Remove() at count 0")
	}

	// Close while timer is pending — should prevent StopFunc.
	if err := lc.Close(); err != nil {
		t.Fatalf("Close() returned error: %v", err)
	}

	// Wait well past the shutdown delay.
	time.Sleep(200 * time.Millisecond)

	if v := atomic.LoadInt64(&stopped); v != 0 {
		t.Errorf("StopFunc called %d times after Close(); want 0", v)
	}
}

// ---------------------------------------------------------------------------
// TestLifecycleCloseIdempotent
//
// Calling Close() twice must not panic and must return nil.
// ---------------------------------------------------------------------------

func TestLifecycleCloseIdempotent(t *testing.T) {

	lc := NewDaemonLifecycle(time.Hour, nil)
	lc.Add()

	if err := lc.Close(); err != nil {
		t.Fatalf("first Close() returned error: %v", err)
	}
	if err := lc.Close(); err != nil {
		t.Fatalf("second Close() returned error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestLifecycleTimerActiveState
//
// TimerActive() should reflect the true state of the shutdown timer.
// ---------------------------------------------------------------------------

func TestLifecycleTimerActiveState(t *testing.T) {

	lc := NewDaemonLifecycle(200*time.Millisecond, nil)

	// Initially: count=0, no timer yet → TimerActive is false.
	if lc.TimerActive() {
		t.Error("TimerActive should be false on fresh lifecycle")
	}

	lc.Add() // count = 1
	if lc.TimerActive() {
		t.Error("TimerActive should be false when count > 0")
	}

	lc.Remove() // count = 0, timer starts
	if !lc.TimerActive() {
		t.Error("TimerActive should be true immediately after Remove() at 0")
	}

	// Re-add cancels timer.
	lc.Add()
	if lc.TimerActive() {
		t.Error("TimerActive should be false after Add() cancels timer")
	}

	// Let timer fire, then check it's false.
	lc.Remove()                        // count = 0, timer restarts
	time.Sleep(400 * time.Millisecond) // wait for timer to fire
	if lc.TimerActive() {
		t.Error("TimerActive should be false after timer has fired")
	}
}

// ---------------------------------------------------------------------------
// TestLifecycleStopFuncNeverDeadlocks
//
// StopFunc that performs its own work (not calling back into the
// lifecycle) should complete without deadlock. Uses a channel to
// coordinate.
// ---------------------------------------------------------------------------

func TestLifecycleStopFuncNeverDeadlocks(t *testing.T) {

	done := make(chan struct{})
	lc := NewDaemonLifecycle(30*time.Millisecond, func() error {
		close(done)
		return nil
	})

	lc.Add()
	lc.Remove()

	select {
	case <-done:
		// StopFunc ran and returned — good.
	case <-time.After(2 * time.Second):
		t.Fatal("StopFunc never completed (possible deadlock)")
	}
}

// ---------------------------------------------------------------------------
// TestLifecycleMultipleCycles
//
// Repeated Add/Remove cycles where the timer is repeatedly cancelled
// should not eventually fire the StopFunc.
// ---------------------------------------------------------------------------

func TestLifecycleMultipleCycles(t *testing.T) {

	var stopped int64
	delay := 80 * time.Millisecond
	lc := NewDaemonLifecycle(delay, func() error {
		atomic.AddInt64(&stopped, 1)
		return nil
	})

	for i := 0; i < 5; i++ {
		lc.Add()                          // 0 → 1
		lc.Remove()                       // 1 → 0, shutdown timer starts
		time.Sleep(20 * time.Millisecond) // let timer tick a bit
		lc.Add()                          // 0 → 1, timer cancelled (daemon stays alive)
		lc.Remove()                       // 1 → 0, timer starts again (next cycle begins from 0)
	}

	// The last Remove left a pending timer; it should fire exactly once.
	select {
	case <-time.After(2 * delay):
		t.Fatal("StopFunc never called after final Remove")
	case <-waitUntilStopped(t, &stopped):
	}

	if v := atomic.LoadInt64(&stopped); v != 1 {
		t.Errorf("StopFunc called %d times; want exactly 1", v)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// waitUntilStopped returns a channel that closes when the atomic counter
// becomes > 0. Used for synchronizing on StopFunc invocation.
func waitUntilStopped(t *testing.T, counter *int64) <-chan struct{} {
	t.Helper()
	ch := make(chan struct{})
	go func() {
		for atomic.LoadInt64(counter) == 0 {
			time.Sleep(5 * time.Millisecond)
		}
		close(ch)
	}()
	return ch
}
