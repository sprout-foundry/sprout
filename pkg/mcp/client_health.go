package mcp

import (
	"context"
	"fmt"
	"time"
)

// ping sends a ping request to check if the server is responsive
func (c *MCPClient) ping(ctx context.Context) error {
	c.mutex.RLock()
	stdin := c.stdin
	c.mutex.RUnlock()

	if stdin == nil {
		return fmt.Errorf("stdin not available")
	}

	_, err := c.sendRequest(ctx, "ping", nil)
	return err
}

// startHealthCheck starts the health check goroutine
func (c *MCPClient) startHealthCheck() {
	// Derive health check context from client's context
	healthCtx, healthCancel := context.WithCancel(c.ctx)
	c.healthCheckCtx = healthCtx
	c.healthCheckCancel = healthCancel

	go func() {
		ticker := time.NewTicker(c.healthInterval)
		defer ticker.Stop()

		for {
			select {
			case <-healthCtx.Done():
				// Health check stopped
				return
			case <-ticker.C:
				c.mutex.RLock()
				running := c.running && !c.stopping
				c.mutex.RUnlock()

				if !running {
					// Server not running or stopping, skip health check
					continue
				}

				// Send ping
				ctx, cancel := context.WithTimeout(healthCtx, 10*time.Second)
				if err := c.ping(ctx); err != nil {
					cancel()
					// Health check failed, trigger reconnection
					c.mutex.Lock()
					if c.running && !c.stopping {
						if c.logger != nil {
							c.logger.LogProcessStep(fmt.Sprintf("[WARN] Health check failed for MCP server %s: %v", c.config.Name, err))
						}
						go c.reconnect(healthCtx)
					}
					c.mutex.Unlock()
				} else {
					cancel()
					// Health check passed, check if we should reset backoff
					c.mutex.Lock()
					if c.reconnectAttempt > 0 && time.Since(c.connectedAt) > 2*time.Minute {
						// Connection has been stable for 2 minutes, reset backoff and failure history
						if c.logger != nil {
							c.logger.LogProcessStep(fmt.Sprintf("[OK] Connection stable for MCP server %s, resetting backoff and failure history", c.config.Name))
						}
						c.reconnectAttempt = 0
						c.failureTimestamps = nil
					}
					c.mutex.Unlock()
				}
			}
		}
	}()
}
