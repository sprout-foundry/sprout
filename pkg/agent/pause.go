package agent

import (
	"context"
	"os"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
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

	a.pauseMutex.Lock()
	defer a.pauseMutex.Unlock()

	// Initialize pause state if needed
	if a.pauseState == nil {
		a.pauseState = &PauseState{}
	}

	// Set pause state
	a.pauseState.IsPaused = true
	a.pauseState.PausedAt = time.Now()

	// Store current messages for context restoration
	a.pauseState.MessagesBefore = make([]api.Message, len(a.messages))
	copy(a.pauseState.MessagesBefore, a.messages)

	// Interrupt handling is deterministic:
	// any interrupt request stops the current task immediately without prompting.
	if os.Getenv("LEDIT_FROM_AGENT") == "1" {
		a.debugLog("Subagent interrupt detected, stopping task\n")
	}
	a.pauseState.IsPaused = false
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
