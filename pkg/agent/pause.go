package agent

import (
	"context"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// snapshotInterrupt returns the current interruptCtx and interruptCancel under
// lock. Callers should operate on the returned snapshot rather than touching
// the fields directly, so concurrent ClearInterrupt /
// resetInterruptForNewQuery don't race with reads.
func (a *Agent) snapshotInterrupt() (context.Context, context.CancelFunc) {
	a.interruptMu.Lock()
	defer a.interruptMu.Unlock()
	return a.interruptCtx, a.interruptCancel
}

// TriggerInterrupt manually triggers an interrupt for testing purposes
func (a *Agent) TriggerInterrupt() {
	_, cancel := a.snapshotInterrupt()
	if cancel != nil {
		cancel()
	}
}

// CheckForInterrupt checks if an interrupt was requested
func (a *Agent) CheckForInterrupt() bool {
	ctx, _ := a.snapshotInterrupt()
	if ctx == nil {
		return false
	}
	select {
	case <-ctx.Done():
		// Context cancelled, interrupt requested
		return true
	default:
		return false
	}
}

// HandleInterrupt processes an interrupt request.
func (a *Agent) HandleInterrupt() string {
	if !a.CheckForInterrupt() {
		return ""
	}

	pauseMutex := a.state.GetPauseMutex()
	pauseMutex.Lock()
	defer pauseMutex.Unlock()

	// Initialize pause state if needed
	pauseState := a.state.GetPauseState()
	if pauseState == nil {
		pauseState = &PauseState{}
		a.state.SetPauseState(pauseState)
	}

	// Set pause state
	pauseState.IsPaused = true
	pauseState.PausedAt = time.Now()

	// Store current messages for context restoration
	messages := a.state.GetMessages()
	pauseState.MessagesBefore = make([]api.Message, len(messages))
	copy(pauseState.MessagesBefore, messages)
	a.state.SetPauseState(pauseState)

	// Interrupt handling is deterministic:
	// any interrupt request stops the current task immediately without prompting.
	if a.IsSubagent() {
		a.Logger().Debug("Subagent interrupt detected, stopping task\n")
	}
	pauseState.IsPaused = false
	a.state.SetPauseState(pauseState)
	a.ClearInterrupt()
	a.Logger().Debug("HandleInterrupt: Returning STOP\n")
	return "STOP"
}

// ClearInterrupt resets the interrupt state
func (a *Agent) ClearInterrupt() {
	base := a.parentInterruptCtx
	if base == nil {
		base = context.Background()
	}
	newCtx, newCancel := context.WithCancel(base)
	a.interruptMu.Lock()
	oldCancel := a.interruptCancel
	a.interruptCtx = newCtx
	a.interruptCancel = newCancel
	a.interruptMu.Unlock()
	// Cancel the previous ctx outside the lock so any callbacks invoked from
	// the cancellation can re-enter the agent without deadlocking on interruptMu.
	if oldCancel != nil {
		oldCancel()
	}
}

// resetInterruptForNewQuery ensures the interruptCtx is fresh at the start
// of a new ProcessQuery. If the previous query was stopped via TriggerInterrupt,
// the cancelled ctx would otherwise persist and instantly abort the next
// HTTP request (now that SP-034-1e wires interruptCtx into the request body).
// Idempotent — if the ctx is already non-cancelled, this is essentially a no-op.
//
// For subagents, the new context is derived from parentInterruptCtx (the
// parent's runCtx) so that cancelling the parent still propagates into the
// subagent's LLM calls after the reset. Without this, the first
// resetInterruptForNewQuery call inside ProcessQuery would sever the parent
// linkage established by createSubagent, making the subagent un-cancellable
// again — the exact deadlock we fixed.
func (a *Agent) resetInterruptForNewQuery() {
	a.interruptMu.Lock()
	if a.interruptCtx != nil {
		select {
		case <-a.interruptCtx.Done():
			// Previous query was cancelled — make a fresh ctx.
		default:
			// Still live; leave it alone.
			a.interruptMu.Unlock()
			return
		}
	}
	base := a.parentInterruptCtx
	if base == nil {
		base = context.Background()
	}
	newCtx, newCancel := context.WithCancel(base)
	a.interruptCtx = newCtx
	a.interruptCancel = newCancel
	a.interruptMu.Unlock()
}
