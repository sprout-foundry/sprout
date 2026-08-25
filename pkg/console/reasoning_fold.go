package console

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// Named constants for the render loop.
const (
	reasoningRefreshInterval = 100 * time.Millisecond
	bytesPerTokenEstimate    = 4
)

// ReasoningFold tracks reasoning output and renders a live-updating
// "⋯ thinking · N tokens · T elapsed" line on the ActivityIndicator row.
// When resolved, it prints a permanent "⋯ thought for N tokens · T elapsed"
// line into the scrollback.
type ReasoningFold struct {
	indicator *ActivityIndicator
	mu        sync.Mutex

	startedAt     time.Time
	tokenEstimate int
	active        bool
	resolved      bool // track if Resolve() has been called for current burst

	// Update goroutine control (TTY mode only).
	updateStopCh chan struct{}
	updateDoneCh chan struct{}

	// Non-TTY state.
	nonTTYFirstChunkPrinted bool
}

// NewReasoningFold creates a fold instance. If indicator is nil or not a TTY,
// it operates in degraded mode (single Fprintln per burst + summary).
func NewReasoningFold(indicator *ActivityIndicator) *ReasoningFold {
	return &ReasoningFold{
		indicator: indicator,
	}
}

// isTTYMode reports whether the fold should use the in-place TTY display path.
func (f *ReasoningFold) isTTYMode() bool {
	return f.indicator != nil && f.indicator.IsTTY()
}

// Start begins a new reasoning burst. Resets token estimate and elapsed time.
// On TTY: pins the indicator row with SetStatic showing the initial state.
// On non-TTY: prints one line immediately.
// Multiple Start() calls in one session produce independent resolved lines.
func (f *ReasoningFold) Start() {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Stop any previous update goroutine.
	if f.updateStopCh != nil {
		close(f.updateStopCh)
		<-f.updateDoneCh
		f.updateStopCh = nil
		f.updateDoneCh = nil
	}

	f.startedAt = time.Now()
	f.tokenEstimate = 0
	f.active = true
	f.resolved = false

	if f.isTTYMode() {
		f.indicator.SetStatic("⋯ thinking · 0 tokens · 0.0s")
		// Spawn the refresh goroutine.
		f.updateStopCh = make(chan struct{})
		f.updateDoneCh = make(chan struct{})
		go f.updateLoop()
	} else {
		if !f.nonTTYFirstChunkPrinted {
			fmt.Fprintln(os.Stderr, "⋯ thinking...")
			f.nonTTYFirstChunkPrinted = true
		}
	}
}

// Chunk receives a reasoning text chunk. Updates token estimate (len(text)/4)
// and refreshes the display. On TTY: updates SetStatic every ~100ms via the
// ticker goroutine. On non-TTY: no-op (already printed at Start).
func (f *ReasoningFold) Chunk(text string) {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.active {
		return
	}
	f.tokenEstimate += len(text) / bytesPerTokenEstimate
}

// updateLoop runs on TTY mode, refreshing the indicator line every
// reasoningRefreshInterval until the stop channel is closed.
func (f *ReasoningFold) updateLoop() {
	stopCh := f.updateStopCh
	doneCh := f.updateDoneCh
	defer close(doneCh)

	ticker := time.NewTicker(reasoningRefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			f.mu.Lock()
			if !f.active {
				f.mu.Unlock()
				return
			}
			tokens := f.tokenEstimate
			elapsed := time.Since(f.startedAt).Seconds()
			f.mu.Unlock()

			f.indicator.SetStatic(fmt.Sprintf("⋯ thinking · %d tokens · %.1fs", tokens, elapsed))
		}
	}
}

// Resolve finalizes the current reasoning burst. On TTY: clears the indicator
// row (ClearStatic), prints permanent summary into scrollback. On non-TTY:
// prints summary line. Idempotent — second call is no-op.
func (f *ReasoningFold) Resolve() {
	f.mu.Lock()
	if f.resolved {
		f.mu.Unlock()
		return
	}
	f.resolved = true
	f.active = false

	// Stop the update goroutine.
	if f.updateStopCh != nil {
		close(f.updateStopCh)
		<-f.updateDoneCh
		f.updateStopCh = nil
		f.updateDoneCh = nil
	}

	tokens := f.tokenEstimate
	elapsed := time.Since(f.startedAt).Seconds()
	f.mu.Unlock()

	// Clear the indicator row and print permanent summary.
	if f.indicator != nil && f.indicator.IsTTY() {
		f.indicator.ClearStatic()
	}
	fmt.Fprintf(os.Stderr, "⋯ thought for %d tokens · %.1fs\n", tokens, elapsed)

	// Reset non-TTY flag so the next Start() prints its own header.
	f.mu.Lock()
	f.nonTTYFirstChunkPrinted = false
	f.mu.Unlock()
}

// Interrupt handles Ctrl+C mid-reasoning. Prints "⋯ thinking interrupted (N tokens)"
// and clears pinned state. Idempotent.
func (f *ReasoningFold) Interrupt() {
	f.mu.Lock()
	if !f.active {
		f.mu.Unlock()
		return
	}
	f.active = false

	// Stop the update goroutine.
	if f.updateStopCh != nil {
		close(f.updateStopCh)
		<-f.updateDoneCh
		f.updateStopCh = nil
		f.updateDoneCh = nil
	}

	tokens := f.tokenEstimate
	f.mu.Unlock()

	// Clear the indicator row and print interrupt summary.
	if f.indicator != nil && f.indicator.IsTTY() {
		f.indicator.ClearStatic()
	}
	fmt.Fprintf(os.Stderr, "⋯ thinking interrupted (%d tokens)\n", tokens)

	// Reset non-TTY flag.
	f.mu.Lock()
	f.nonTTYFirstChunkPrinted = false
	f.mu.Unlock()
}

// IsActive reports whether the fold is currently tracking an active reasoning
// burst (started but not yet resolved or interrupted).
func (f *ReasoningFold) IsActive() bool {
	if f == nil {
		return false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.active && !f.resolved
}
