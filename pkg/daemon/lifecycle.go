//go:build !js

package daemon

import (
	"sync"
	"time"
)

// DaemonLifecycle manages the connection lifetime of a daemon process.
//
// Connections are tracked via Add/Remove. When the last connection
// disconnects, a shutdown delay timer starts. If a new connection arrives
// before the timer fires, the timer is cancelled. When the timer fires
// (with no active connections), the StopFunc is invoked exactly once.
//
// Typical flow:
//
//	lc := NewDaemonLifecycle(60*time.Second, func() error {
//	    return daemonProcess.Kill()
//	})
//	lc.Add() // first connection
//	lc.Add() // second connection (count → 2)
//	lc.Remove() // one disconnects (count → 1)
//	lc.Remove() // last disconnect (count → 0, 60s timer starts)
//	lc.Add() // new connection (count → 1, timer cancelled)
//	lc.Close() // clean up when shutting down
type DaemonLifecycle struct {
	mu            sync.Mutex
	count         int
	stopFunc      func() error
	shutdownDelay time.Duration
	timer         *time.Timer
	timerActive   bool      // timer created and pending (not yet fired or stopped)
	stopped       bool      // Close has been called
	stoppedOnce   sync.Once // ensure StopFunc runs at most once
}

// NewDaemonLifecycle creates a lifecycle manager with the given shutdown
// delay and stop function. The stop function is called exactly once when
// the last connection disconnects and the delay expires.
func NewDaemonLifecycle(shutdownDelay time.Duration, stopFunc func() error) *DaemonLifecycle {
	return &DaemonLifecycle{
		stopFunc:      stopFunc,
		shutdownDelay: shutdownDelay,
	}
}

// Add registers a new active connection. Returns the new count.
// If the shutdown delay timer is pending (count was 0), the timer
// is stopped — the daemon stays alive.
func (l *DaemonLifecycle) Add() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	// If the timer is pending (we were at 0 and counting down),
	// stop it so the daemon stays alive.
	if l.timer != nil && l.timerActive {
		l.timer.Stop()
		l.timerActive = false
	}

	l.count++
	return l.count
}

// Remove deregisters a connection. Returns the new count.
// If the count reaches 0, the shutdown delay timer starts.
func (l *DaemonLifecycle) Remove() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.count > 0 {
		l.count--
	}

	if l.count == 0 {
		l.startTimer()
	}

	return l.count
}

// Count returns the current number of active connections.
func (l *DaemonLifecycle) Count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.count
}

// TimerActive reports whether the shutdown delay timer is currently
// pending (count is 0 and timer hasn't fired yet). Useful for tests
// to verify the timer was started correctly.
func (l *DaemonLifecycle) TimerActive() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.count == 0 && l.timerActive
}

// Close stops the internal timer and releases resources. It is safe
// to call multiple times and does NOT invoke StopFunc (callers
// manage the daemon process lifecycle independently).
func (l *DaemonLifecycle) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.timer != nil {
		l.timer.Stop()
		l.timerActive = false
	}
	l.stopped = true
	return nil
}

// startTimer creates and starts the shutdown delay timer. Must be called
// with l.mu held.
func (l *DaemonLifecycle) startTimer() {
	// Cancel any existing timer (shouldn't normally happen when count==0,
	// but be defensive).
	if l.timer != nil {
		l.timer.Stop()
	}

	l.timer = time.AfterFunc(l.shutdownDelay, func() {
		l.onTimerFired()
	})
	l.timerActive = true
}

// onTimerFired is called when the shutdown delay timer fires.
// It re-checks the count under the mutex: if a connection arrived
// during teardown, it aborts. Otherwise it calls StopFunc exactly once.
func (l *DaemonLifecycle) onTimerFired() {
	l.stoppedOnce.Do(func() {
		l.mu.Lock()
		defer l.mu.Unlock()

		// If a connection arrived during teardown, don't stop.
		if l.count > 0 || l.stopped {
			return
		}

		l.timerActive = false

		if l.stopFunc != nil {
			_ = l.stopFunc()
		}
	})
}
