package agent

import (
	"context"
	"time"
)

// Shutdown attempts to gracefully stop background work and child processes
// (e.g., MCP servers), and releases resources. It is safe to call multiple times.
func (a *Agent) Shutdown() {
	if a == nil {
		return
	}
	a.initSubManagers()

	// Save command history to configuration before shutdown.
	if a.state != nil {
		a.state.GetHistoryMutex().Lock()
		a.saveHistoryToConfig()
		a.state.GetHistoryMutex().Unlock()
	}

	// Stop MCP servers (best-effort)
	if mgr := a.mcpSub.GetManager(); mgr != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = mgr.StopAll(ctx)
		cancel()
	}

	// Cancel interrupt context
	if a.interruptCancel != nil {
		a.interruptCancel()
	}

	// Close async output worker
	if ch := a.output.GetAsyncOutput(); ch != nil {
		close(ch)
		a.output.SetAsyncOutput(nil)
	}

	// Close debug log file
	if a.debugLogFile != nil {
		_ = a.debugLogFile.Close()
		a.debugLogFile = nil
	}

	// Close embedding manager resources
	if a.embeddingMgr != nil {
		_ = a.embeddingMgr.Close()
		a.embeddingMgr = nil
	}
}

// SetInterruptHandler sets the interrupt handler for UI mode
func (a *Agent) SetInterruptHandler(ch chan struct{}) {
	// Store the channel for external interrupt handling
	// Note: This is kept for backward compatibility
	// Interrupts are now primarily handled via context cancellation
}

// InterruptCtx returns the agent's interrupt context so child operations
// (e.g., tool execution) can derive from it and respect user cancellations.
func (a *Agent) InterruptCtx() context.Context {
	return a.interruptCtx
}
