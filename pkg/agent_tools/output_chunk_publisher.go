//go:build !js

package tools

import (
	"bytes"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

const (
	// coalesceInterval is the time threshold for publishing a chunk event.
	// When 250ms has elapsed since the last publish, accumulated output is flushed.
	coalesceInterval = 250 * time.Millisecond

	// coalesceThreshold is the byte threshold for publishing a chunk event.
	// When 4KB has accumulated since the last publish, output is flushed regardless
	// of elapsed time.
	coalesceThreshold = 4 * 1024
)

// OutputChunkPublisher implements io.Writer. It accumulates bytes from
// a background process's stdout/stderr and publishes automate.output_chunk
// events on a time-and-size coalesced basis (≥250ms or ≥4KB) so that
// WebSocket frames aren't overwhelmed by rapid small writes.
type OutputChunkPublisher struct {
	sessionID    string
	eventBus     *events.EventBus
	buf          bytes.Buffer
	totalWritten int64
	lastPublish  time.Time
	mu           sync.Mutex
}

// NewOutputChunkPublisher creates a publisher that streams output-chunk
// events for the given session ID via the provided event bus.
func NewOutputChunkPublisher(sessionID string, eventBus *events.EventBus) *OutputChunkPublisher {
	return &OutputChunkPublisher{
		sessionID: sessionID,
		eventBus:  eventBus,
	}
}

// Write accumulates bytes from the writer chain. It triggers a publish
// event when the coalescing thresholds are met (≥250ms since last publish
// or ≥4KB accumulated since last publish). Implements io.Writer.
func (p *OutputChunkPublisher) Write(data []byte) (int, error) {
	n := len(data)
	if n == 0 {
		return 0, nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.buf.Write(data)

	if p.shouldPublishLocked() {
		p.publishLocked()
	}

	return n, nil
}

// Flush publishes any remaining accumulated bytes. Call this when the
// backing process exits so the last bits of output reach subscribers.
// Safe to call when there is nothing to flush (no-op).
func (p *OutputChunkPublisher) Flush() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.buf.Len() > 0 {
		p.publishLocked()
	}
}

// shouldPublishLocked checks the coalescing thresholds. Caller must hold mu.
func (p *OutputChunkPublisher) shouldPublishLocked() bool {
	accumulated := p.buf.Len()
	if accumulated == 0 {
		return false
	}
	if accumulated >= coalesceThreshold {
		return true
	}
	if !p.lastPublish.IsZero() && time.Since(p.lastPublish) >= coalesceInterval {
		return true
	}
	return false
}

// publishLocked drains the buffer into an automate.output_chunk event.
// Caller must hold mu.
func (p *OutputChunkPublisher) publishLocked() {
	if p.eventBus == nil {
		return
	}
	chunk := p.buf.String()
	p.buf.Reset()

	offset := p.totalWritten
	p.totalWritten += int64(len(chunk))
	p.lastPublish = time.Now()

	p.eventBus.Publish(events.EventTypeAutomateOutputChunk, events.AutomateOutputChunkEvent(
		p.sessionID, int(offset), chunk,
	))
}
