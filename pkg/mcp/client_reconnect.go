package mcp

import (
	"bufio"
	"context"
	"fmt"
	"time"
)

// triggerReconnect checks if a reconnection should be attempted after the
// message handler exits unexpectedly, and spawns the reconnect goroutine.
func (c *MCPClient) triggerReconnect(reason string, err error) {
	c.mutex.RLock()
	stopping := c.stopping
	running := c.running
	c.mutex.RUnlock()

	if stopping || !running {
		return
	}

	if c.logger != nil {
		if err != nil {
			c.logger.LogProcessStep(fmt.Sprintf("[WARN] MCP server %s %s: %v", c.config.Name, reason, err))
		} else {
			c.logger.LogProcessStep(fmt.Sprintf("[WARN] MCP server %s %s", c.config.Name, reason))
		}
	}

	c.mutex.Lock()
	if c.running && !c.stopping {
		clientCtx := c.ctx
		go c.reconnect(clientCtx)
	}
	c.mutex.Unlock()
}

// handleErrors handles stderr output from the server
func (c *MCPClient) handleErrors() {
	// Capture stderr under lock — Stop() / tests may swap it to nil concurrently.
	c.mutex.RLock()
	stderr := c.stderr
	c.mutex.RUnlock()
	if stderr == nil {
		return
	}
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" && c.logger != nil {
			c.logger.LogProcessStep(fmt.Sprintf("[STDERR] MCP server %s stderr: %s", c.config.Name, line))
		}
	}
}

// reconnect attempts to reconnect to the MCP server with exponential backoff
func (c *MCPClient) reconnect(ctx context.Context) {
	// Track failures in 60s window for adaptive backoff (set inside lock, used after unlock)
	var failuresIn60s int

	c.mutex.Lock()

	// Record this failure and check sliding windows
	c.failureTimestamps = append(c.failureTimestamps, time.Now())
	now := time.Now()
	var pruned []time.Time
	for _, t := range c.failureTimestamps {
		if now.Sub(t) <= 24*time.Hour {
			pruned = append(pruned, t)
		}
	}
	c.failureTimestamps = pruned

	// Check 24-hour window: if >= 10 failures, disable the server
	failuresIn24h := len(c.failureTimestamps)
	if failuresIn24h >= 10 {
		c.disabled = true
		c.disabledReason = "10 failures in 24 hours"
		c.reconnecting = false
		c.mutex.Unlock()
		if c.logger != nil {
			c.logger.LogProcessStep(fmt.Sprintf("[DISABLED] MCP server %s disabled after %d failures in 24 hours", c.config.Name, failuresIn24h))
		}
		return
	}

	// Count 60-second window failures for adaptive backoff
	failuresIn60s = 0
	for _, t := range c.failureTimestamps {
		if now.Sub(t) <= 60*time.Second {
			failuresIn60s++
		}
	}

	if c.stopping || c.reconnecting || c.reconnectAttempt >= c.getMaxRestarts() {
		c.mutex.Unlock()
		if c.logger != nil {
			if c.stopping {
				c.logger.LogProcessStep(fmt.Sprintf("[INFO] Skipping reconnect for %s (server stopping)", c.config.Name))
			} else if c.reconnecting {
				c.logger.LogProcessStep(fmt.Sprintf("[INFO] Skipping reconnect for %s (reconnect already in progress)", c.config.Name))
			} else {
				c.logger.LogProcessStep(fmt.Sprintf("[ERROR] Max reconnect attempts (%d) reached for MCP server %s", c.getMaxRestarts(), c.config.Name))
			}
		}
		return
	}

	c.reconnecting = true
	c.reconnectAttempt++
	attempt := c.reconnectAttempt
	c.mutex.Unlock()
	defer func() {
		c.mutex.Lock()
		c.reconnecting = false
		c.mutex.Unlock()
	}()

	// Calculate backoff delay
	delay := c.calculateBackoff(attempt)
	// Boost backoff for rapid failure patterns (>3 failures in 60s)
	if failuresIn60s > 3 {
		delay = delay * 2
	}
	if c.logger != nil {
		c.logger.LogProcessStep(fmt.Sprintf("[RECONNECT] Attempting reconnect %d/%d for MCP server %s in %v", attempt, c.getMaxRestarts(), c.config.Name, delay))
	}

	// Wait for backoff delay
	select {
	case <-time.After(delay):
	case <-ctx.Done():
		if c.logger != nil {
			c.logger.LogProcessStep(fmt.Sprintf("[RECONNECT] Reconnect cancelled for MCP server %s", c.config.Name))
		}
		return
	}

	// Terminate old process and clean up before restarting.
	// Nil out fields under lock to prevent double-close races with concurrent Stop().
	c.mutex.Lock()
	oldCancel := c.cancel
	oldStdin := c.stdin
	oldStdout := c.stdout
	oldStderr := c.stderr
	c.stdin = nil
	c.stdout = nil
	c.stderr = nil
	c.cancel = nil
	// Clear health check state so startInternal() will create a fresh one (MUST_FIX #3)
	c.healthCheckCancel = nil
	c.healthCheckCtx = nil
	c.mutex.Unlock()

	// Cancel the old context to signal old goroutines to stop
	if oldCancel != nil {
		oldCancel()
	}
	// Close old pipes to unblock old goroutines (safe to call nil-check since we nulled above)
	if oldStdin != nil {
		oldStdin.Close()
	}
	if oldStdout != nil {
		oldStdout.Close()
	}
	if oldStderr != nil {
		oldStderr.Close()
	}
	// Brief sleep to let old goroutines detect EOF and exit
	time.Sleep(50 * time.Millisecond)

	// Mark as not running before attempting restart
	// Clear pending requests to prevent stale response delivery
	c.reqMutex.Lock()
	for id, ch := range c.pendingReqs {
		close(ch)
		delete(c.pendingReqs, id)
	}
	c.reqMutex.Unlock()

	c.mutex.Lock()
	c.running = false
	c.initialized = false
	c.mutex.Unlock()

	// Start the server again via startInternal (bypasses reconnecting guard).
	// This will increment restartCount and create a new health check goroutine.
	//
	// Pass ctx directly — startInternal creates its own cancellable child
	// context (c.ctx/c.cancel). Wrapping ctx in WithCancel with defer cancel()
	// here would cancel that child the moment reconnect() returns, instantly
	// killing the process we just started.

	// startInternal() sets connectedAt = time.Now(), which the health check
	// uses for the 2-minute stability reset of failureTimestamps.
	if err := c.startInternal(ctx); err != nil {
		c.mutex.Lock()
		c.running = false
		c.mutex.Unlock()

		if c.logger != nil {
			c.logger.LogProcessStep(fmt.Sprintf("[ERROR] Reconnect attempt %d failed for MCP server %s: %v", attempt, c.config.Name, err))
		}

		// Don't retry here - the health check will trigger another attempt if needed
		return
	}

	// Re-initialize the server after successful connection
	if err := c.Initialize(ctx); err != nil {
		c.mutex.Lock()
		c.running = false
		c.mutex.Unlock()

		if c.logger != nil {
			c.logger.LogProcessStep(fmt.Sprintf("[ERROR] Failed to initialize after reconnect for MCP server %s: %v", c.config.Name, err))
		}
		return
	}

	if c.logger != nil {
		c.logger.LogProcessStep(fmt.Sprintf("[OK] Successfully reconnected and initialized MCP server %s (attempt %d)", c.config.Name, attempt))
	}

	// Reset reconnect budget after successful reconnect+initialize so that
	// subsequent crashes get a fresh retry budget instead of accumulating
	// toward the max-restarts cap.
	c.mutex.Lock()
	c.reconnectAttempt = 0
	c.mutex.Unlock()
}

// calculateBackoff calculates exponential backoff delay
func (c *MCPClient) calculateBackoff(attempt int) time.Duration {
	// Start with 1 second, double each attempt up to max 5 minutes
	backoff := time.Duration(1<<uint(attempt-1)) * time.Second
	if backoff > 5*time.Minute {
		backoff = 5 * time.Minute
	}
	return backoff
}

// getMaxRestarts returns the maximum number of restart attempts
func (c *MCPClient) getMaxRestarts() int {
	if c.config.MaxRestarts > 0 {
		return c.config.MaxRestarts
	}
	return 3 // default, matching pkg/mcp/config.go
}
