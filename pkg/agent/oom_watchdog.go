//go:build !js

package agent

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

const bytesPerGB = 1024 * 1024 * 1024

const (
	defaultWatchdogInterval   = 30 * time.Second
	defaultNodeCountThreshold = 500
	defaultRSSThresholdBytes  = 50 * bytesPerGB // 50 GB
	defaultCooldownDuration   = 5 * time.Minute
)

// OOMProbeResult holds the result of a single OOM probe scan.
type OOMProbeResult struct {
	NodeCount     int
	TotalRSSBytes uint64
	Timestamp     time.Time
}

// OOMWatchdog monitors Node.js process count and total RSS via /proc
// scanning. It alerts BEFORE the kernel OOM-killer fires by publishing
// events when thresholds are exceeded.
type OOMWatchdog struct {
	eventBus           *events.EventBus
	interval           time.Duration
	nodeCountThreshold int
	rssThresholdBytes  uint64
	cooldownDuration   time.Duration
	probeFn            func() (*OOMProbeResult, error)
	mu                 sync.Mutex
	lastAlertState     string // "none", "node_count", "rss", "both"
	lastAlertTime      time.Time
}

// NewOOMWatchdog creates a watchdog with sensible defaults.
func NewOOMWatchdog(eventBus *events.EventBus) *OOMWatchdog {
	return &OOMWatchdog{
		eventBus:           eventBus,
		interval:           defaultWatchdogInterval,
		nodeCountThreshold: defaultNodeCountThreshold,
		rssThresholdBytes:  defaultRSSThresholdBytes,
		cooldownDuration:   defaultCooldownDuration,
		lastAlertState:     "none",
	}
}

// Start launches a background goroutine that probes at the configured
// interval. The goroutine exits when ctx is cancelled.
func (w *OOMWatchdog) Start(ctx context.Context) {
	go w.run(ctx)
}

// Stop is a no-op. The watchdog goroutine is driven by context
// cancellation — call cancel() on the context passed to Start() to
// stop it. This method exists for API symmetry with other watchdog
// interfaces.
func (w *OOMWatchdog) Stop() {
	// Actual stop is handled by cancelling the context passed to Start().
}

func (w *OOMWatchdog) run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	// Run the first probe immediately so we don't wait a full interval.
	w.doProbe()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.doProbe()
		}
	}
}

func (w *OOMWatchdog) doProbe() {
	w.mu.Lock()
	fn := w.probeFn
	w.mu.Unlock()
	if fn == nil {
		fn = w.probe
	}

	result, err := fn()
	if err != nil {
		log.Printf("[OOMWatchdog] probe error: %v", err)
		return
	}

	w.evaluate(result)
}

// evaluate checks the probe result against thresholds and emits alerts
// with cooldown/throttling logic.
func (w *OOMWatchdog) evaluate(result *OOMProbeResult) {
	// Determine current trigger state.
	nodeExceeded := result.NodeCount >= w.nodeCountThreshold
	rssExceeded := result.TotalRSSBytes >= w.rssThresholdBytes

	var currentState string
	switch {
	case nodeExceeded && rssExceeded:
		currentState = "both"
	case nodeExceeded:
		currentState = "node_count"
	case rssExceeded:
		currentState = "rss"
	default:
		currentState = "none"
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	// No thresholds exceeded → reset cooldown state.
	if currentState == "none" {
		w.lastAlertState = "none"
		return
	}

	// State changed from last alert → emit immediately.
	if currentState != w.lastAlertState {
		w.emitAlertLocked(result, currentState)
		return
	}

	// Same state: check cooldown.
	if time.Since(w.lastAlertTime) >= w.cooldownDuration {
		w.emitAlertLocked(result, currentState)
	}
	// Otherwise suppress (cooldown active).
}

// emitAlertLocked publishes the alert event and logs it.
// Caller must hold w.mu.
func (w *OOMWatchdog) emitAlertLocked(result *OOMProbeResult, state string) {
	w.lastAlertState = state
	w.lastAlertTime = time.Now()

	var message string
	switch state {
	case "node_count":
		message = fmt.Sprintf("Node process count (%d) >= threshold (%d)",
			result.NodeCount, w.nodeCountThreshold)
	case "rss":
		message = fmt.Sprintf("Total RSS (%.1f GB) >= threshold (%.1f GB)",
			float64(result.TotalRSSBytes)/bytesPerGB,
			float64(w.rssThresholdBytes)/bytesPerGB)
	case "both":
		message = fmt.Sprintf("Node count (%d) >= %d AND RSS (%.1f GB) >= %.1f GB",
			result.NodeCount, w.nodeCountThreshold,
			float64(result.TotalRSSBytes)/bytesPerGB,
			float64(w.rssThresholdBytes)/bytesPerGB)
	default:
		message = fmt.Sprintf("OOM watchdog alert: %s", state)
	}

	log.Printf("[OOMWatchdog] ALERT: %s", message)

	w.eventBus.Publish(events.EventTypeOOMWatchdogAlert, events.OOMWatchdogAlertEvent(
		result.NodeCount,
		result.TotalRSSBytes,
		w.nodeCountThreshold,
		w.rssThresholdBytes,
		state,
	))
}

// setProbeFunc is used by tests to inject a mock probe function.
// Not exported; tests in package agent can access it.
func (w *OOMWatchdog) setProbeFunc(fn func() (*OOMProbeResult, error)) {
	w.mu.Lock()
	w.probeFn = fn
	w.mu.Unlock()
}
