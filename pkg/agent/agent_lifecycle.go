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

	// Save command history to configuration before shutdown.
	// saveHistoryToConfig reads state via getters (which are individually
	// thread-safe) and calls configManager.UpdateConfig for persistence.
	// No HistoryMutex is needed here — the lock ordering risk
	// (HistoryMutex → configLock) is avoided by not holding the lock
	// during the I/O call, matching the pattern in AddToHistory.
	if a.state != nil {
		a.saveHistoryToConfig()
	}

	// Stop MCP servers (best-effort)
	if a.mcpSub != nil {
		if mgr := a.mcpSub.GetManager(); mgr != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = mgr.StopAll(ctx)
			cancel()
		}
	}

	// Cancel interrupt context
	if _, cancel := a.snapshotInterrupt(); cancel != nil {
		cancel()
	}

	// Wait for background goroutines (memory migration, etc.) to finish
	// before closing the resources they depend on.
	a.backgroundWg.Wait()

	// Close async output worker
	if a.output != nil {
		if ch := a.output.GetAsyncOutput(); ch != nil {
			close(ch)
			a.output.SetAsyncOutput(nil)
		}
	}

	// Close debug log file
	if a.debugLogFile != nil {
		_ = a.debugLogFile.Close()
		a.debugLogFile = nil
	}

	// Close embedding manager resources
	a.embeddingMu.Lock()
	mgr := a.embeddingMgr
	a.embeddingMgr = nil
	a.embeddingMu.Unlock()
	if mgr != nil {
		_ = mgr.Close()
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
	ctx, _ := a.snapshotInterrupt()
	return ctx
}
