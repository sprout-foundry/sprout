package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"strings"
)

// handleMessages handles incoming messages from the server
func (c *MCPClient) handleMessages() {
	// Capture stdout under lock — Stop() / tests may swap it to nil concurrently.
	c.mutex.RLock()
	stdout := c.stdout
	c.mutex.RUnlock()
	if stdout == nil {
		return
	}
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		// Skip lines that don't start with { (not JSON)
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			// Non-JSON output (warnings, logs, etc.) - skip silently
			continue
		}

		var message MCPMessage
		if err := json.Unmarshal([]byte(line), &message); err != nil {
			if c.logger != nil {
				c.logger.LogProcessStep(fmt.Sprintf("[WARN] Failed to parse MCP message from %s: %v", c.config.Name, err))
			}
			continue
		}

		// Handle responses to our requests
		if message.ID != nil {
			idStr := fmt.Sprintf("%v", message.ID)
			c.reqMutex.RLock()
			if responseChan, exists := c.pendingReqs[idStr]; exists {
				c.reqMutex.RUnlock()
				// Protect against send on closed channel if reconnect/Stop
				// closes the channel between our RUnlock and this send.
				func() {
					defer func() {
						recover() //nolint:errcheck // safe to swallow send-on-closed-channel
					}()
					select {
					case responseChan <- message:
					default:
					}
				}()
			} else {
				c.reqMutex.RUnlock()
			}
		}
		// Handle notifications/events (ID is nil)
		// Could be extended to handle server notifications in the future
	}

	// Scanner ended - check if this was unexpected (process died)
	if err := scanner.Err(); err != nil {
		c.triggerReconnect("stdout scanner ended unexpectedly", err)
	} else {
		// Scanner ended without error (EOF)
		c.triggerReconnect("stdout closed unexpectedly (EOF)", nil)
	}
}
