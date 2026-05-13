package agent

import (
	"context"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// TriggerInterrupt manually triggers an interrupt for testing purposes
func (a *Agent) TriggerInterrupt() {
	if a.interruptCancel != nil {
		a.interruptCancel()
	}
}

// CheckForInterrupt checks if an interrupt was requested
func (a *Agent) CheckForInterrupt() bool {
	select {
	case <-a.interruptCtx.Done():
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
		a.debugLog("Subagent interrupt detected, stopping task\n")
	}
	pauseState.IsPaused = false
	a.state.SetPauseState(pauseState)
	a.ClearInterrupt()
	a.debugLog("HandleInterrupt: Returning STOP\n")
	return "STOP"
}

// ClearInterrupt resets the interrupt state
func (a *Agent) ClearInterrupt() {
	// Create new interrupt context
	if a.interruptCancel != nil {
		a.interruptCancel()
	}
	interruptCtx, interruptCancel := context.WithCancel(context.Background())
	a.interruptCtx = interruptCtx
	a.interruptCancel = interruptCancel
}
