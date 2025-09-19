package utils

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// HangDetector monitors operations and detects potential hangs
type HangDetector struct {
	name            string
	timeout         time.Duration
	heartbeatTicker *time.Ticker
	lastActivity    time.Time
	isActive        bool
	mu              sync.RWMutex
	ctx             context.Context
	cancel          context.CancelFunc
	onHangDetected  func(string, time.Duration)
}

// NewHangDetector creates a new hang detector
func NewHangDetector(name string, timeout time.Duration) *HangDetector {
	ctx, cancel := context.WithCancel(context.Background())

	detector := &HangDetector{
		name:           name,
		timeout:        timeout,
		lastActivity:   time.Now(),
		isActive:       false,
		ctx:            ctx,
		cancel:         cancel,
		onHangDetected: defaultHangHandler,
	}

	return detector
}

// Start begins monitoring for hangs
func (hd *HangDetector) Start() {
	hd.mu.Lock()
	defer hd.mu.Unlock()

	if hd.isActive {
		return
	}

	hd.isActive = true
	hd.lastActivity = time.Now()
	hd.heartbeatTicker = time.NewTicker(hd.timeout / 4) // Check 4 times per timeout period

	go hd.monitor()

	if rl := GetRunLogger(); rl != nil {
		rl.LogEvent("hang_detector_start", map[string]any{
			"detector":   hd.name,
			"timeout_ms": hd.timeout.Milliseconds(),
			"started_at": time.Now().Format(time.RFC3339),
		})
	}
}

// Stop stops the hang detector
func (hd *HangDetector) Stop() {
	hd.mu.Lock()
	defer hd.mu.Unlock()

	if !hd.isActive {
		return
	}

	hd.isActive = false
	if hd.heartbeatTicker != nil {
		hd.heartbeatTicker.Stop()
	}
	hd.cancel()

	if rl := GetRunLogger(); rl != nil {
		duration := time.Since(hd.lastActivity)
		rl.LogEvent("hang_detector_stop", map[string]any{
			"detector":          hd.name,
			"final_duration_ms": duration.Milliseconds(),
			"stopped_at":        time.Now().Format(time.RFC3339),
		})
	}
}

// Heartbeat signals that the operation is still active
func (hd *HangDetector) Heartbeat(activity string) {
	hd.mu.Lock()
	defer hd.mu.Unlock()

	hd.lastActivity = time.Now()

	if rl := GetRunLogger(); rl != nil {
		rl.LogEvent("hang_detector_heartbeat", map[string]any{
			"detector":  hd.name,
			"activity":  activity,
			"timestamp": hd.lastActivity.Format(time.RFC3339),
		})
	}
}

// SetHangHandler sets a custom handler for when a hang is detected
func (hd *HangDetector) SetHangHandler(handler func(string, time.Duration)) {
	hd.mu.Lock()
	defer hd.mu.Unlock()
	hd.onHangDetected = handler
}

// monitor runs the hang detection loop
func (hd *HangDetector) monitor() {
	defer func() {
		if hd.heartbeatTicker != nil {
			hd.heartbeatTicker.Stop()
		}
	}()

	for {
		select {
		case <-hd.ctx.Done():
			return
		case <-hd.heartbeatTicker.C:
			hd.checkForHang()
		}
	}
}

// checkForHang checks if operation has hung
func (hd *HangDetector) checkForHang() {
	hd.mu.RLock()
	lastActivity := hd.lastActivity
	isActive := hd.isActive
	hd.mu.RUnlock()

	if !isActive {
		return
	}

	timeSinceActivity := time.Since(lastActivity)
	if timeSinceActivity > hd.timeout {
		// Potential hang detected
		if hd.onHangDetected != nil {
			hd.onHangDetected(hd.name, timeSinceActivity)
		}

		if rl := GetRunLogger(); rl != nil {
			rl.LogEvent("hang_detected", map[string]any{
				"detector":               hd.name,
				"time_since_activity_ms": timeSinceActivity.Milliseconds(),
				"timeout_ms":             hd.timeout.Milliseconds(),
				"detected_at":            time.Now().Format(time.RFC3339),
			})
		}

		// Log to workspace log as well for visibility
		logger := GetLogger(false)
		logger.LogProcessStep(fmt.Sprintf("🚨 HANG DETECTED: No progress heartbeat from %s for %v (timeout: %v)",
			hd.name, timeSinceActivity, hd.timeout))
	}
}

// defaultHangHandler is the default handler when a hang is detected
func defaultHangHandler(name string, duration time.Duration) {
	fmt.Printf("⚠️  HANG DETECTED: No progress updates from %s for %v\n", name, duration)
	fmt.Printf("    This may indicate the operation is stuck. Use Ctrl+C to interrupt if needed.\n")
}

// ProgressMonitor wraps operations with automatic hang detection
type ProgressMonitor struct {
	detector *HangDetector
	name     string
}

// NewProgressMonitor creates a progress monitor for an operation
func NewProgressMonitor(name string, timeout time.Duration) *ProgressMonitor {
	return &ProgressMonitor{
		detector: NewHangDetector(name, timeout),
		name:     name,
	}
}

// Start begins monitoring
func (pm *ProgressMonitor) Start() {
	pm.detector.Start()
}

// Stop ends monitoring
func (pm *ProgressMonitor) Stop() {
	pm.detector.Stop()
}

// Progress signals progress with a description
func (pm *ProgressMonitor) Progress(description string) {
	pm.detector.Heartbeat(description)
}

// WithProgress wraps a function with progress monitoring
func (pm *ProgressMonitor) WithProgress(description string, fn func() error) error {
	pm.Progress(description)
	return fn()
}
