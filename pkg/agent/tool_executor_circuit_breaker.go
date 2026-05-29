// Circuit breaker: prevents infinite tool-execution loops by tracking
// repeated identical actions within a sliding time window.
package agent

import (
	"encoding/json"
	"fmt"
)

// checkCircuitBreaker checks if an action should be blocked
func (te *ToolExecutor) checkCircuitBreaker(toolName string, args map[string]interface{}) bool {
	if te.agent.state == nil || te.agent.state.GetCircuitBreaker() == nil {
		return false
	}

	key := te.generateActionKey(toolName, args)

	// Read the Count under the lock — escaping the *CircuitBreakerAction
	// pointer out of RLock scope and reading Count afterward would be a TOCTOU
	// race against updateCircuitBreaker's writes to the same field.
	actionCount, exists := func() (int, bool) {
		cb := te.agent.state.GetCircuitBreaker()
		cb.mu.RLock()
		defer cb.mu.RUnlock()
		a, ok := cb.Actions[key]
		if !ok {
			return 0, false
		}
		return a.Count, true
	}()

	if !exists {
		return false
	}

	// Higher threshold for troubleshooting operations
	threshold := 3

	// Increase threshold for common troubleshooting operations
	switch toolName {
	case "read_file":
		// Reading files is often repeated during troubleshooting
		threshold = 5
		// But be more aggressive for ZAI to prevent loops
		if te.agent.GetProvider() == "zai" {
			threshold = 3
		}
	case "shell_command":
		// Shell commands are frequently repeated during troubleshooting and debugging
		threshold = 8
	case "edit_file":
		// Editing the same file multiple times might be needed for complex fixes
		threshold = 4
	}

	// Block if attempted too many times
	return actionCount >= threshold
}

// updateCircuitBreaker updates the circuit breaker state
// The caller expects this function to be thread-safe with respect to the circuitBreaker map.
func (te *ToolExecutor) updateCircuitBreaker(toolName string, args map[string]interface{}) {
	if te.agent.state == nil || te.agent.state.GetCircuitBreaker() == nil {
		return
	}

	key := te.generateActionKey(toolName, args)
	cb := te.agent.state.GetCircuitBreaker()
	cb.mu.Lock()
	defer cb.mu.Unlock()

	action, exists := cb.Actions[key]
	if !exists {
		action = &CircuitBreakerAction{
			ActionType: toolName,
			Target:     key,
			Count:      0,
		}
		cb.Actions[key] = action
	}

	action.Count++
	action.LastUsed = getCurrentTime()

	// Clean up old entries (older than 5 minutes) to prevent memory leaks
	te.cleanupOldCircuitBreakerEntriesLocked()
}

// cleanupOldCircuitBreakerEntriesLocked removes entries older than 5 minutes
// Precondition: caller must hold te.agent.state.GetCircuitBreaker().mu.Lock()
func (te *ToolExecutor) cleanupOldCircuitBreakerEntriesLocked() {
	currentTime := getCurrentTime()
	fiveMinutesAgo := currentTime - 300 // 5 minutes in seconds

	cb := te.agent.state.GetCircuitBreaker()
	for key, action := range cb.Actions {
		if action.LastUsed < fiveMinutesAgo {
			delete(cb.Actions, key)
		}
	}
}

// cleanupOldCircuitBreakerEntries removes entries older than 5 minutes
// This function handles locking internally and is safe to call from anywhere.
func (te *ToolExecutor) cleanupOldCircuitBreakerEntries() {
	if te.agent.state.GetCircuitBreaker() == nil {
		return
	}

	cb := te.agent.state.GetCircuitBreaker()
	cb.mu.Lock()
	defer cb.mu.Unlock()
	te.cleanupOldCircuitBreakerEntriesLocked()
}

// generateActionKey creates a unique key for an action
func (te *ToolExecutor) generateActionKey(toolName string, args map[string]interface{}) string {
	// Create a deterministic key from tool name and arguments
	argsJSON, _ := json.Marshal(args)
	return fmt.Sprintf("%s:%s", toolName, string(argsJSON))
}
