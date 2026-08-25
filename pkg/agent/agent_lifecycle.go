package agent

import (
	"context"
	"time"

	"github.com/sprout-foundry/sprout/pkg/embedding"
)

// Shutdown attempts to gracefully stop background work and child processes
// (e.g., MCP servers), and releases resources. It is safe to call multiple times.
func (a *Agent) Shutdown() {
	if a == nil {
		return
	}
	a.shutdownOnce.Do(a.shutdownLocked)
}

// IsShutdown reports whether Shutdown() has completed. The WebUI releases
// agents on background goroutines (workspace switch, chat deletion, idle
// eviction), so callers that need teardown to have finished — flushed history,
// closed embedding store, stopped MCP servers — have to be able to observe it.
func (a *Agent) IsShutdown() bool {
	if a == nil {
		return true
	}
	return a.shutdown.Load()
}

func (a *Agent) shutdownLocked() {
	defer a.shutdown.Store(true)

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

	// Cancel lifetime context so background watchers (wakeup, etc.) stop.
	if a.lifetimeCancel != nil {
		a.lifetimeCancel()
	}

	// Cancel interrupt context
	if _, cancel := a.snapshotInterrupt(); cancel != nil {
		cancel()
	}

	// Stop background process manager (CLI-mode background shells)
	if a.backgroundProcessManager != nil {
		a.backgroundProcessManager.Close()
		a.backgroundProcessManager = nil
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

	// Release embedding manager resources. The manager is shared across every
	// agent on this workspace, so it is closed by the last releaser, not here.
	a.embeddingMu.Lock()
	mgr := a.embeddingMgr
	a.embeddingMgr = nil
	a.embeddingMu.Unlock()
	embedding.ReleaseManager(mgr)
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
