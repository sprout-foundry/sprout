package agent

import (
	"context"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// snapshotInterrupt returns the current interruptCtx and interruptCancel under lock. Callers operate on the snapshot to avoid races.
func (a *Agent) snapshotInterrupt() (context.Context, context.CancelFunc) {
	a.interruptMu.Lock()
	defer a.interruptMu.Unlock()
	return a.interruptCtx, a.interruptCancel
}

// TriggerInterrupt manually triggers an interrupt for testing purposes
func (a *Agent) TriggerInterrupt() {
	a.DisableWakeup()
	_, cancel := a.snapshotInterrupt()
	if cancel != nil {
		cancel()
	}
	// Preempt running subagents too. The ctx cancellation above propagates
	// to their in-flight LLM calls, but a wedged subagent (retry cascade,
	// stuck tool) never surfaces it; CancelSubagent additionally fires the
	// subagent's own interrupt and tears down its run context so runTask's
	// 5s cancellation window can reclaim the parent's turn. Without this,
	// Ctrl+C during a hung subagent leaves the turn blocked until the
	// subagent's 30-minute timeout.
	a.cancelRunningSubagents()
}

// cancelRunningSubagents preempts every active subagent owned by this
// agent's runner. Nil-safe and safe when no runner/subagents exist.
func (a *Agent) cancelRunningSubagents() {
	if a.subagentRunner == nil {
		return
	}
	a.subagentRunner.CancelAll()
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

// HandleInterrupt processes an interrupt request. Deterministic: any interrupt stops the current task immediately.
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

	pauseState.IsPaused = true
	pauseState.PausedAt = time.Now()

	messages := a.state.GetMessages()
	pauseState.MessagesBefore = make([]api.Message, len(messages))
	copy(pauseState.MessagesBefore, messages)
	a.state.SetPauseState(pauseState)

	// Interrupt stops the current task immediately without prompting.
	if a.IsSubagent() {
		a.Logger().Debug("Subagent interrupt detected, stopping task\n")
	}
	pauseState.IsPaused = false
	a.state.SetPauseState(pauseState)
	a.ClearInterrupt()
	a.Logger().Debug("HandleInterrupt: Returning STOP\n")
	return "STOP"
}

// ClearInterrupt resets the interrupt state. Cancels the previous ctx outside the lock to allow callbacks to re-enter.
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
	// Cancel the previous ctx outside the lock so callbacks can re-enter without deadlocking on interruptMu.
	if oldCancel != nil {
		oldCancel()
	}
}

// resetInterruptForNewQuery ensures the interruptCtx is fresh at the start of a new ProcessQuery.
// For subagents, derives from parentInterruptCtx so cancelling the parent still propagates.
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
