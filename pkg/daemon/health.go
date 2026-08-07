//go:build !js

// Package daemon provides client-side health monitoring for the sprout daemon.
//
// This is the Phase-1 client-side health-detection mechanism for SP-136.
// The CLI-on-daemon feature (Phase 2+) will call MonitorDaemonHealth before
// or while using the daemon.  The fallback callback should switch the caller
// to in-process execution.  No wiring into cmd/agent_command.go is done yet
// — the mechanism is built as a standalone, well-tested package ready for
// Phase 2 integration.
package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// ---------------------------------------------------------------------------
// HealthStatus — mirrors the /health endpoint response
// ---------------------------------------------------------------------------

// HealthStatus is decoded from the JSON response of GET /health.
// Fields map 1:1 to the fields returned by the daemon's health endpoint.
// Unknown or missing fields are silently ignored (defensive decoding).
type HealthStatus struct {
	Status         string `json:"status"`
	Port           int    `json:"port"`
	Uptime         string `json:"uptime"`
	AgentAvailable bool   `json:"agent_available"`
	ActiveQueries  int    `json:"active_queries"`
}

// ---------------------------------------------------------------------------
// CheckHealth — one-shot health check
// ---------------------------------------------------------------------------

// CheckHealth performs a single GET against baseURL+"/health" with the given
// timeout.  On HTTP 200 the JSON body is decoded into a HealthStatus and
// returned.  Any other status code, network error, or JSON decode error
// returns a descriptive error.
//
// Use http.NewRequestWithContext internally so the caller's context
// cancellation is respected.
func CheckHealth(ctx context.Context, baseURL string, timeout time.Duration) (*HealthStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/health", nil)
	if err != nil {
		return nil, fmt.Errorf("create health request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("health check request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("health check returned HTTP %d", resp.StatusCode)
	}

	var status HealthStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("decode health response: %w", err)
	}
	return &status, nil
}

// ---------------------------------------------------------------------------
// HealthMonitor — periodic health monitoring with failure threshold
// ---------------------------------------------------------------------------

// DefaultHealthInterval is the default interval between health checks (5s).
const DefaultHealthInterval = 5 * time.Second

// DefaultHealthTimeout is the default timeout for each individual health check (2s).
const DefaultHealthTimeout = 2 * time.Second

// DefaultFailureThreshold is the default number of consecutive failures
// before the fallback callback is invoked (3).
const DefaultFailureThreshold = 3

// HealthOption configures a HealthMonitor.
type HealthOption func(*HealthMonitor)

// WithInterval sets the polling interval between health checks.
func WithInterval(d time.Duration) HealthOption {
	return func(m *HealthMonitor) { m.interval = d }
}

// WithTimeout sets the timeout for each individual health check.
func WithTimeout(d time.Duration) HealthOption {
	return func(m *HealthMonitor) { m.timeout = d }
}

// WithFailureThreshold sets the number of consecutive failures before
// the fallback callback is invoked.
func WithFailureThreshold(n int) HealthOption {
	return func(m *HealthMonitor) { m.failureThreshold = n }
}

// WithLogger sets a custom logger for health check output.
func WithLogger(logger *slog.Logger) HealthOption {
	return func(m *HealthMonitor) { m.logger = logger }
}

// WithWarningFunc sets a callback invoked on each health check failure
// (before the threshold is reached).
func WithWarningFunc(fn func(string)) HealthOption {
	return func(m *HealthMonitor) { m.warningFunc = fn }
}

// WithFallbackFunc sets a callback invoked when the failure threshold is
// reached.  The callback receives a descriptive reason string.
func WithFallbackFunc(fn func(reason string)) HealthOption {
	return func(m *HealthMonitor) { m.fallbackFunc = fn }
}

// HealthMonitor periodically pings the daemon's /health endpoint and
// tracks consecutive failures.  On reaching the failure threshold, it
// invokes the fallback callback (e.g. to switch to in-process execution).
//
// The monitor is safe for concurrent use: Start/Stop are idempotent,
// and ConsecutiveFailures returns the latest atomic value.
type HealthMonitor struct {
	baseURL          string
	interval         time.Duration
	timeout          time.Duration
	failureThreshold int
	logger           *slog.Logger
	warningFunc      func(string)
	fallbackFunc     func(reason string)

	consecutiveFailures int64 // atomic

	startOnce sync.Once
	stopOnce  sync.Once
	stopCh    chan struct{}
	stopDone  chan struct{} // closed when the goroutine exits
}

// NewHealthMonitor creates a monitor configured with the given baseURL
// and options.  Default values: interval=5s, timeout=2s, threshold=3.
// The monitor does not start automatically — call Start(ctx).
func NewHealthMonitor(baseURL string, opts ...HealthOption) *HealthMonitor {
	m := &HealthMonitor{
		baseURL:          baseURL,
		interval:         DefaultHealthInterval,
		timeout:          DefaultHealthTimeout,
		failureThreshold: DefaultFailureThreshold,
		logger:           slog.Default(),
		stopCh:           make(chan struct{}),
		stopDone:         make(chan struct{}),
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Start launches the health-check goroutine.  It polls /health on the
// configured interval until ctx is cancelled or Stop() is called.
//
// Start is idempotent: calling it twice is safe (the second call is a no-op).
// The startOnce field (not a local once) guarantees only ONE goroutine ever
// runs, so stopDone is closed exactly once.
func (m *HealthMonitor) Start(ctx context.Context) {
	m.startOnce.Do(func() {
		go m.run(ctx)
	})
}

// Stop signals the monitor goroutine to exit.  It is safe to call multiple
// times (sync.Once) and does not block on the goroutine exiting.
func (m *HealthMonitor) Stop() {
	m.stopOnce.Do(func() {
		close(m.stopCh)
	})
}

// WaitStop blocks until the monitor goroutine has exited.  Safe to call
// after Stop() or after the context passed to Start() is cancelled.
func (m *HealthMonitor) WaitStop() {
	<-m.stopDone
}

// ConsecutiveFailures returns the current consecutive-failure count
// (atomic read).  Useful for tests to assert failure tracking.
func (m *HealthMonitor) ConsecutiveFailures() int {
	return int(atomic.LoadInt64(&m.consecutiveFailures))
}

func (m *HealthMonitor) run(ctx context.Context) {
	defer close(m.stopDone)

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	// Perform an initial check immediately so the caller gets fast feedback.
	m.check(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.check(ctx)
		}
	}
}

func (m *HealthMonitor) check(ctx context.Context) {
	status, err := CheckHealth(ctx, m.baseURL, m.timeout)
	if err != nil {
		failures := atomic.AddInt64(&m.consecutiveFailures, 1)
		m.logger.Warn("daemon health check failed",
			slog.Int("consecutive_failures", int(failures)),
			slog.Any("err", err),
		)
		if m.warningFunc != nil {
			m.warningFunc(fmt.Sprintf("daemon health check failed (%d consecutive): %v", int(failures), err))
		}
		if int(failures) >= m.failureThreshold && m.fallbackFunc != nil {
			reason := fmt.Sprintf("daemon unhealthy after %d consecutive failed health checks: %v; falling back to in-process execution", int(failures), err)
			m.fallbackFunc(reason)
		}
		return
	}

	// Success — reset failure counter.
	prev := atomic.SwapInt64(&m.consecutiveFailures, 0)
	if prev > 0 {
		m.logger.Info("daemon health recovered",
			slog.String("status", status.Status),
			slog.Int("previous_failures", int(prev)),
		)
	}
}

// ---------------------------------------------------------------------------
// MonitorDaemonHealth — convenience: default monitor, Start, return it
// ---------------------------------------------------------------------------

// MonitorDaemonHealth creates a HealthMonitor with default settings, starts
// it, and returns it.  The fallback callback is invoked when the failure
// threshold is reached.  Warnings are logged via slog.
//
// The returned monitor should be stopped (or its context cancelled) when
// the caller no longer needs monitoring.  Example:
//
//	mon := MonitorDaemonHealth(ctx, "http://localhost:56000", func(reason string) {
//	    // switch to in-process execution
//	})
//	defer mon.Stop()
func MonitorDaemonHealth(ctx context.Context, baseURL string, fallback func(reason string)) *HealthMonitor {
	m := NewHealthMonitor(baseURL,
		WithFallbackFunc(fallback),
	)
	m.Start(ctx)
	return m
}
