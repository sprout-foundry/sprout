//go:build !js

package daemon

import (
	"sync"
	"time"
)

// DaemonActivity tracks whether a daemon subsystem is in use so the idle
// reaper (cmd/daemon_idle.go) can see socket-server traffic it would
// otherwise be blind to.
//
// Two signals feed Idle:
//   - Begin/End maintain an in-flight request counter; any request being
//     served counts as activity no matter how long it runs.
//   - Touch (and Begin/End) record the last-activity timestamp, so a request
//     that just completed still counts as activity until the window elapses —
//     bursty callers don't get the daemon torn down between requests.
type DaemonActivity struct {
	mu         sync.Mutex
	inFlight   int
	lastActive time.Time
}

// NewDaemonActivity returns an activity tracker with no activity recorded.
func NewDaemonActivity() *DaemonActivity {
	return &DaemonActivity{}
}

// Begin marks a request as in flight.
func (a *DaemonActivity) Begin() {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.inFlight++
	a.lastActive = time.Now()
}

// End marks an in-flight request complete.
func (a *DaemonActivity) End() {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.inFlight > 0 {
		a.inFlight--
	}
	a.lastActive = time.Now()
}

// Touch records that activity happened now.
func (a *DaemonActivity) Touch() {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lastActive = time.Now()
}

// Idle reports whether the subsystem has no in-flight work and has seen no
// activity within window.
func (a *DaemonActivity) Idle(now time.Time, window time.Duration) bool {
	if a == nil {
		return true
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.inFlight > 0 {
		return false
	}
	if a.lastActive.IsZero() {
		return true
	}
	return now.Sub(a.lastActive) >= window
}
