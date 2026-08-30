//go:build !js

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// TestCheckHealthOK — successful 200 response
// ---------------------------------------------------------------------------

func TestCheckHealthOK(t *testing.T) {

	body := HealthStatus{
		Status:         "ok",
		Port:           56000,
		Uptime:         "1m",
		AgentAvailable: true,
		ActiveQueries:  2,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	status, err := CheckHealth(ctx, srv.URL, 1*time.Second)
	require.NoError(t, err)
	require.NotNil(t, status)

	assert.Equal(t, "ok", status.Status)
	assert.Equal(t, 56000, status.Port)
	assert.Equal(t, "1m", status.Uptime)
	assert.True(t, status.AgentAvailable)
	assert.Equal(t, 2, status.ActiveQueries)
}

// ---------------------------------------------------------------------------
// TestCheckHealthNon200 — server returns 500
// ---------------------------------------------------------------------------

func TestCheckHealthNon200(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := CheckHealth(ctx, srv.URL, 1*time.Second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

// ---------------------------------------------------------------------------
// TestCheckHealthConnectionRefused — no listener on port
// ---------------------------------------------------------------------------

func TestCheckHealthConnectionRefused(t *testing.T) {

	// Grab a free port, close the listener, so nothing is listening.
	// NOTE: the port can be re-bound by a concurrent listener in another
	// test/package between Close() and Connect(), which would make the
	// check succeed instead of failing. Retry with a fresh port until we
	// observe a genuine refusal (or the port keeps getting stolen).
	for attempt := 0; attempt < 5; attempt++ {
		var l net.Listener
		var err error
		for i := 0; i < 50; i++ { // retry transient ephemeral exhaustion
			l, err = net.Listen("tcp", "127.0.0.1:0")
			if err == nil {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		require.NoError(t, err, "net.Listen on ephemeral port")
		addr := l.Addr().String()
		l.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, err = CheckHealth(ctx, "http://"+addr, 1*time.Second)
		cancel()
		if err != nil {
			return // expected: connection refused (or any connection error)
		}
	}
	t.Fatal("connection to a closed port was never refused after 5 attempts " +
		"(the freed port kept getting reused by concurrent listeners)")
}

// ---------------------------------------------------------------------------
// TestCheckHealthTimeout — handler sleeps longer than the timeout
// ---------------------------------------------------------------------------

func TestCheckHealthTimeout(t *testing.T) {

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Bound total test time so the slow handler doesn't hold up the suite.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	deadlineCtx, deadlineCancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer deadlineCancel()

	start := time.Now()
	_, err := CheckHealth(deadlineCtx, srv.URL, 50*time.Millisecond)
	elapsed := time.Since(start)

	require.Error(t, err)
	// The 50ms client timeout should fire well before the 300ms handler.
	assert.Less(t, elapsed.Milliseconds(), int64(200),
		"check should timeout quickly (took %v)", elapsed)
}

// ---------------------------------------------------------------------------
// TestHealthMonitorFiresFallbackAfterThreshold
//
// With threshold=3 the fallback fires on failures 3, 4, 5, …
// (every check whose consecutive count >= threshold).
// ---------------------------------------------------------------------------

func TestHealthMonitorFiresFallbackAfterThreshold(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	var fallbackCalls int64
	var lastReason string
	var lastReasonMu sync.Mutex

	m := NewHealthMonitor(
		srv.URL,
		WithInterval(10*time.Millisecond),
		WithTimeout(50*time.Millisecond),
		WithFailureThreshold(3),
		WithFallbackFunc(func(reason string) {
			atomic.AddInt64(&fallbackCalls, 1)
			lastReasonMu.Lock()
			lastReason = reason
			lastReasonMu.Unlock()
		}),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	m.Start(ctx)
	defer m.Stop()

	// Wait until ConsecutiveFailures reaches the threshold.
	if !pollUntil(t, 500*time.Millisecond, 10*time.Millisecond, func() bool {
		return m.ConsecutiveFailures() >= 3
	}) {
		t.Fatal("timed out waiting for threshold")
	}

	lastReasonMu.Lock()
	reason := lastReason
	callsSoFar := atomic.LoadInt64(&fallbackCalls)
	lastReasonMu.Unlock()

	assert.GreaterOrEqual(t, callsSoFar, int64(1), "fallback should have fired at least once")
	assert.Contains(t, reason, "unhealthy")
	assert.Contains(t, reason, "falling back")
	assert.GreaterOrEqual(t, m.ConsecutiveFailures(), 3)

	// Wait for a few more checks — fallback fires again because it uses >= .
	time.Sleep(30 * time.Millisecond)

	callsAfter := atomic.LoadInt64(&fallbackCalls)
	assert.Greater(t, callsAfter, callsSoFar,
		"fallback should fire on every check >= threshold (current: 3, 4, 5, …)")
}

// ---------------------------------------------------------------------------
// TestHealthMonitorRecoversAfterFailures
//
// Server fails while the test hasn't yet observed the failure count reach
// 3, then starts succeeding. Gate on the OBSERVED count rather than a
// fixed request budget: with a 10ms check interval, a fixed 3-request
// budget makes ">= 3" observable for a single ~10ms window before the
// counter swaps back to 0 — a loaded runner's 10ms poll can step over
// that window entirely and the phase-1 wait times out even though
// recovery worked.
// ---------------------------------------------------------------------------

func TestHealthMonitorRecoversAfterFailures(t *testing.T) {
	var phase1Done atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !phase1Done.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(HealthStatus{Status: "ok", Port: 56000})
	}))
	defer srv.Close()

	m := NewHealthMonitor(
		srv.URL,
		WithInterval(10*time.Millisecond),
		WithTimeout(50*time.Millisecond),
		WithFailureThreshold(5),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	m.Start(ctx)
	defer m.Stop()

	// Phase 1: wait for failures to accumulate (≥ 3).
	if !pollUntil(t, 500*time.Millisecond, 10*time.Millisecond, func() bool {
		return m.ConsecutiveFailures() >= 3
	}) {
		t.Fatal("timed out waiting for initial failures")
	}
	// Let the gate open only after phase 1 has observed the count.
	phase1Done.Store(true)

	// Phase 2: wait for recovery (ConsecutiveFailures → 0).
	if !pollUntil(t, 1*time.Second, 10*time.Millisecond, func() bool {
		return m.ConsecutiveFailures() == 0
	}) {
		t.Fatalf("timed out waiting for recovery; ConsecutiveFailures = %d",
			m.ConsecutiveFailures())
	}
}

// ---------------------------------------------------------------------------
// TestHealthMonitorStopIdempotent
//
// Calling Stop() twice must not panic; WaitStop should return promptly.
// ---------------------------------------------------------------------------

func TestHealthMonitorStopIdempotent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	m := NewHealthMonitor(srv.URL, WithInterval(10*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m.Start(ctx)

	// Stop twice — must not panic.
	assert.NotPanics(t, func() {
		m.Stop()
		m.Stop()
	})

	// WaitStop should return within 1s.
	select {
	case <-m.stopDone:
		// goroutine exited cleanly
	case <-time.After(time.Second):
		t.Fatal("WaitStop timed out")
	}
}

// ---------------------------------------------------------------------------
// TestMonitorDaemonHealthConvenience
//
// Verify the convenience function returns a non-nil monitor and that
// fallback fires when the server is failing.
// ---------------------------------------------------------------------------

func TestMonitorDaemonHealthConvenience(t *testing.T) {
	var fallbackFired int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// The convenience function returns a non-nil monitor.
	mon := MonitorDaemonHealth(ctx, srv.URL, func(reason string) {
		atomic.StoreInt64(&fallbackFired, 1)
	})
	require.NotNil(t, mon, "MonitorDaemonHealth should return a non-nil monitor")

	// Defaults (5s interval, threshold 3) are too slow for testing.
	// Use a dedicated short-interval monitor to verify fallback fires.
	m := NewHealthMonitor(
		srv.URL,
		WithInterval(10*time.Millisecond),
		WithTimeout(50*time.Millisecond),
		WithFailureThreshold(3),
		WithFallbackFunc(func(reason string) {
			atomic.StoreInt64(&fallbackFired, 1)
		}),
	)
	m.Start(ctx)
	defer m.Stop()

	if !pollUntil(t, 1*time.Second, 20*time.Millisecond, func() bool {
		return atomic.LoadInt64(&fallbackFired) > 0
	}) {
		t.Fatal("fallback never fired")
	}
}

// ---------------------------------------------------------------------------
// TestHealthMonitorStartIsIdempotent — CRITICAL BUG TEST
//
// The current Start() implementation declares a LOCAL sync.Once on the stack:
//
//	type startState struct{ once sync.Once; started bool }
//	var st startState
//	st.once.Do(func() { … go m.run(ctx) })
//
// Because `st` is a local variable, every call to Start() creates a
// FRESH startState, so each Start() spawns its OWN goroutine.  Two
// goroutines running at the same interval double the check rate and
// cause the fallback to fire from BOTH goroutines.
//
// A CORRECT implementation uses a field-level sync.Once (or mutex) so
// the second Start() is a no-op.
//
// This test is EXPECTED TO FAIL against the current implementation.
// ---------------------------------------------------------------------------

func TestHealthMonitorStartIsIdempotent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	var fallbackCount int64

	m := NewHealthMonitor(
		srv.URL,
		WithInterval(20*time.Millisecond),
		WithTimeout(10*time.Millisecond),
		WithFailureThreshold(3),
		WithFallbackFunc(func(reason string) {
			atomic.AddInt64(&fallbackCount, 1)
		}),
	)

	ctx1, cancel1 := context.WithCancel(context.Background())
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel1()
	defer cancel2()

	// Start twice with separate contexts.
	m.Start(ctx1)
	m.Start(ctx2) // BUG: spawns a second goroutine.

	// Wait ~5 check cycles from a single goroutine:
	//   5 × 20ms = ~100ms, plus the initial check ≈ 6 checks total.
	time.Sleep(110 * time.Millisecond)

	// Cancel both contexts to stop all goroutines.
	cancel1()
	cancel2()
	time.Sleep(50 * time.Millisecond) // let goroutines drain

	// The BUGGY implementation will panic on double-close of stopDone.
	// Wrap Stop in recover to avoid crashing the test suite.
	var doubleClosePanic string
	func() {
		defer func() {
			if r := recover(); r != nil {
				doubleClosePanic = fmt.Sprintf("%v", r)
			}
		}()
		m.Stop()
	}()

	count := atomic.LoadInt64(&fallbackCount)

	// ── expected counts ──
	//
	// Single goroutine (correct idempotent Start):
	//   Checks at t≈0, 20, 40, 60, 80, 100 → ~6 checks in 110ms.
	//   Fallback fires on checks 3, 4, 5, 6 → ~4 fallback calls.
	//   With scheduler jitter, allow up to 6.
	//
	// Two goroutines (buggy local sync.Once):
	//   ~12 checks in 110ms, fallback fires on checks 3-12 → ~10 calls.
	//
	// Threshold: ≤6 = one goroutine; >6 = double goroutines.
	maxExpected := int64(6)

	// Evidence 1: double-close panic proves two goroutines existed.
	if doubleClosePanic != "" {
		t.Logf("EVIDENCE — double-close panic: %s", doubleClosePanic)
	}

	// Evidence 2: fallback count is doubled.
	assert.LessOrEqual(t, count, maxExpected,
		fmt.Sprintf("Start is NOT idempotent: fallback fired %d times (expected ≤ %d). "+
			"Two goroutines are running in parallel, doubling the failure rate. "+
			"If this is ~2× the expected count, Start() is not idempotent and "+
			"spawns multiple goroutines per call.", count, maxExpected))

	// Combined assertion: either a panic OR too many fallback calls proves the bug.
	bugEvidence := []string{}
	if doubleClosePanic != "" {
		bugEvidence = append(bugEvidence,
			fmt.Sprintf("panic on Stop: %s", doubleClosePanic))
	}
	if count > maxExpected {
		bugEvidence = append(bugEvidence,
			fmt.Sprintf("fallback fired %d times (max expected %d)", count, maxExpected))
	}
	if len(bugEvidence) > 0 {
		t.Errorf("Start() is NOT idempotent — evidence: %s",
			strings.Join(bugEvidence, "; "))
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// pollUntil repeatedly evaluates `fn` every `tick` until it returns true
// or the deadline passes.  Returns true on success.
func pollUntil(t *testing.T, deadline, tick time.Duration, fn func() bool) bool {
	t.Helper()
	deadlineTime := time.Now().Add(deadline)
	poll := time.NewTicker(tick)
	defer poll.Stop()
	for {
		if fn() {
			return true
		}
		if time.Now().After(deadlineTime) {
			return false
		}
		select {
		case <-poll.C:
		case <-time.After(deadlineTime.Sub(time.Now())):
		}
	}
}
