package core

import (
	"sync"
	"time"
)

// breakerState represents the state of the circuit breaker.
type breakerState int

const (
	breakerClosed breakerState = iota
	breakerOpen
	breakerHalfOpen
)

// circuitBreaker implements a simple circuit breaker pattern to prevent
// cascading failures when calling unreliable tools or providers.
//
// State machine:
//   - Closed: Normal operation. Failures are counted. When count reaches
//     threshold, transitions to Open.
//   - Open: All requests are rejected immediately. After resetTimeout
//     expires, transitions to Half-Open.
//   - Half-Open: One request is allowed through. If it succeeds, closes.
//     If it fails, reopens.
type circuitBreaker struct {
	mu              sync.Mutex
	failureCount    int
	lastFailureTime time.Time
	threshold       int
	resetTimeout    time.Duration
	state           breakerState
	inFlight        bool // tracks the single allowed request in half-open
}

// newCircuitBreaker creates a circuit breaker with the given threshold and
// reset timeout. Defaults: threshold=5, resetTimeout=30s.
func newCircuitBreaker(threshold int, resetTimeout time.Duration) *circuitBreaker {
	if threshold <= 0 {
		threshold = 5
	}
	if resetTimeout <= 0 {
		resetTimeout = 30 * time.Second
	}
	return &circuitBreaker{
		threshold:    threshold,
		resetTimeout: resetTimeout,
		state:        breakerClosed,
	}
}

// Allow reports whether a request is allowed to proceed.
// In the closed state, always allows. In the open state, allows only after
// resetTimeout has elapsed (transitioning to half-open). In half-open,
// allows exactly one request.
func (cb *circuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case breakerClosed:
		return true
	case breakerOpen:
		// Transition to half-open after timeout.
		if time.Since(cb.lastFailureTime) >= cb.resetTimeout {
			cb.state = breakerHalfOpen
			cb.inFlight = true
			return true
		}
		return false
	case breakerHalfOpen:
		if !cb.inFlight {
			cb.inFlight = true
			return true
		}
		return false
	default:
		return false
	}
}

// RecordSuccess records a successful execution, transitioning to closed.
func (cb *circuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failureCount = 0
	cb.inFlight = false
	cb.state = breakerClosed
}

// RecordFailure records a failed execution. If the failure count reaches
// the threshold while in the closed state, transitions to open.
func (cb *circuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failureCount++
	cb.lastFailureTime = time.Now()
	cb.inFlight = false

	if cb.state == breakerHalfOpen {
		cb.state = breakerOpen
		return
	}

	if cb.failureCount >= cb.threshold && cb.state == breakerClosed {
		cb.state = breakerOpen
	}
}

// State returns the current state of the circuit breaker.
func (cb *circuitBreaker) State() breakerState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}
