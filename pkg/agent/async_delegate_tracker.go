// Package agent async_delegate_tracker provides background execution
// of delegate agents so the parent agent can continue working while
// the delegate runs. Results are collected and returned via
// delegate_status lookups.
package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// asyncDelegateEntry tracks a single async delegate execution.
type asyncDelegateEntry struct {
	ID        string
	Config    DelegateConfig
	Status    string // "running", "completed", "failed"
	Result    *DelegateResult
	StartedAt time.Time
	Done      chan struct{}
	Cancel    context.CancelFunc
}

// AsyncDelegateTracker manages the lifecycle of asynchronous delegate agents.
// Each agent instance gets its own tracker, initialized lazily via initSubManagers.
type AsyncDelegateTracker struct {
	mu      sync.RWMutex
	entries map[string]*asyncDelegateEntry
}

// NewAsyncDelegateTracker creates a new, empty AsyncDelegateTracker.
func NewAsyncDelegateTracker() *AsyncDelegateTracker {
	return &AsyncDelegateTracker{
		entries: make(map[string]*asyncDelegateEntry),
	}
}

// delegateRunFunc is the function signature for executing a delegate in the
// background. It returns a DelegateResult on success or an error on failure.
type delegateRunFunc func(ctx context.Context) (*DelegateResult, error)

// Start begins tracking a new async delegate. The delegate executes via runFn
// in a background goroutine. When it completes or fails, its result is stored
// and the appropriate event is published.
// Returns an error if the delegateID is already tracked (prevents goroutine leak
// from overwriting an in-flight entry).
func (t *AsyncDelegateTracker) Start(delegateID string, cfg DelegateConfig, agent *Agent, runFn delegateRunFunc) error {
	ctx, cancel := context.WithCancel(context.Background())

	entry := &asyncDelegateEntry{
		ID:        delegateID,
		Config:    cfg,
		Status:    "running",
		StartedAt: time.Now(),
		Done:      make(chan struct{}),
		Cancel:    cancel,
	}

	// Capture agent fields under the lock to avoid reading them from a
	// potentially mutated struct in the background goroutine (race fix #2).
	var eventBus *events.EventBus
	var depth int
	if agent != nil {
		eventBus = agent.eventBus
		depth = agent.delegateDepth + 1
	}

	t.mu.Lock()
	if _, exists := t.entries[delegateID]; exists {
		t.mu.Unlock()
		cancel()
		return fmt.Errorf("delegate %q is already tracked", delegateID)
	}
	t.entries[delegateID] = entry
	t.mu.Unlock()

	// Publish delegate_async_started event (using captured values).
	if eventBus != nil {
		event := events.DelegateAsyncEvent(delegateID, "started", cfg.Prompt, depth)
		eventBus.Publish(events.EventTypeDelegateAsyncStarted, event)
	}

	// Run the delegate in a background goroutine.
	go func() {
		defer close(entry.Done)

		result, err := runFn(ctx)

		t.mu.Lock()
		defer t.mu.Unlock()

		if err != nil {
			entry.Status = "failed"
			entry.Result = &DelegateResult{
				Summary:      fmt.Sprintf("Delegate failed: %s", err.Error()),
				ExitStatus:   "error",
				ErrorMessage: err.Error(),
			}
			if eventBus != nil {
				event := events.DelegateAsyncEvent(delegateID, "failed", err.Error(), depth)
				eventBus.Publish(events.EventTypeDelegateAsyncFailed, event)
			}
		} else {
			entry.Status = "completed"
			entry.Result = result
			if eventBus != nil {
				summary := ""
				if result != nil {
					summary = result.Summary
				}
				event := events.DelegateAsyncEvent(delegateID, "completed", summary, depth)
				eventBus.Publish(events.EventTypeDelegateAsyncCompleted, event)
			}
		}
	}()
	return nil
}

// GetStatus returns the current status and result (if available) for a delegate.
// Returns false if the delegate ID is unknown.
func (t *AsyncDelegateTracker) GetStatus(delegateID string) (status string, result *DelegateResult, found bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	entry, ok := t.entries[delegateID]
	if !ok {
		return "", nil, false
	}
	return entry.Status, entry.Result, true
}

// Cancel sends a cancellation signal to a running async delegate.
// Returns true if the delegate was found and was running.
func (t *AsyncDelegateTracker) Cancel(delegateID string) bool {
	t.mu.RLock()
	entry, ok := t.entries[delegateID]
	t.mu.RUnlock()

	if !ok || entry.Status != "running" {
		return false
	}
	entry.Cancel()
	return true
}

// ListRunning returns the IDs of all currently running async delegates.
func (t *AsyncDelegateTracker) ListRunning() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var ids []string
	for id, entry := range t.entries {
		if entry.Status == "running" {
			ids = append(ids, id)
		}
	}
	return ids
}

// Cleanup removes completed/failed entries older than the given TTL.
func (t *AsyncDelegateTracker) Cleanup(ttl time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	for id, entry := range t.entries {
		if entry.Status != "running" && now.Sub(entry.StartedAt) > ttl {
			delete(t.entries, id)
		}
	}
}

// WaitFor blocks until the delegate finishes (up to the context deadline)
// and returns its status and result. Returns an error if the context is
// cancelled or the delegate is not found.
func (t *AsyncDelegateTracker) WaitFor(ctx context.Context, delegateID string) (status string, result *DelegateResult, err error) {
	t.mu.RLock()
	entry, ok := t.entries[delegateID]
	t.mu.RUnlock()

	if !ok {
		return "", nil, fmt.Errorf("delegate %q not found", delegateID)
	}

	if entry.Status != "running" {
		return entry.Status, entry.Result, nil
	}

	select {
	case <-entry.Done:
		// Re-acquire the read lock before reading entry fields.
		// The Done channel was closed under the write lock inside the
		// goroutine, but we must re-lock to ensure a safe read of
		// the now-stable Status and Result fields (race fix #3).
		t.mu.RLock()
		status = entry.Status
		result = entry.Result
		t.mu.RUnlock()
		return status, result, nil
	case <-ctx.Done():
		return "running", nil, ctx.Err()
	}
}
