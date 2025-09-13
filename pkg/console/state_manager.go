package console

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// stateManager implements StateManager interface
type stateManager struct {
	mu            sync.RWMutex
	state         map[string]interface{}
	subscriptions map[string][]subscription
	transaction   *transaction
	idCounter     int
}

// subscription holds callback info
type subscription struct {
	id       string
	pattern  string
	callback StateCallback
}

// transaction holds pending state changes
type transaction struct {
	changes  map[string]interface{}
	original map[string]interface{}
}

// NewStateManager creates a new state manager
func NewStateManager() StateManager {
	return &stateManager{
		state:         make(map[string]interface{}),
		subscriptions: make(map[string][]subscription),
	}
}

// Get retrieves a value from state
func (sm *stateManager) Get(key string) (interface{}, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	// Check transaction first
	if sm.transaction != nil {
		if val, exists := sm.transaction.changes[key]; exists {
			return val, true
		}
	}

	val, exists := sm.state[key]
	return val, exists
}

// Set sets a value in state
func (sm *stateManager) Set(key string, value interface{}) {
	sm.mu.Lock()

	// Get old value before setting
	oldValue := sm.state[key]

	// If in transaction, save to transaction
	if sm.transaction != nil {
		// Save original value if not already saved
		if _, saved := sm.transaction.original[key]; !saved {
			sm.transaction.original[key] = oldValue
		}
		sm.transaction.changes[key] = value
		sm.mu.Unlock()
		return
	}

	// Set value
	sm.state[key] = value

	// Get matching subscriptions
	var callbacks []StateCallback
	for pattern, subs := range sm.subscriptions {
		if sm.matchesPattern(key, pattern) {
			for _, sub := range subs {
				callbacks = append(callbacks, sub.callback)
			}
		}
	}

	sm.mu.Unlock()

	// Call callbacks outside of lock
	for _, callback := range callbacks {
		callback(key, oldValue, value)
	}
}

// Delete removes a value from state
func (sm *stateManager) Delete(key string) {
	sm.mu.Lock()

	// Get old value before deleting
	oldValue := sm.state[key]

	// If in transaction, save to transaction
	if sm.transaction != nil {
		// Save original value if not already saved
		if _, saved := sm.transaction.original[key]; !saved {
			sm.transaction.original[key] = oldValue
		}
		sm.transaction.changes[key] = nil // nil represents deletion
		sm.mu.Unlock()
		return
	}

	// Delete value
	delete(sm.state, key)

	// Get matching subscriptions
	var callbacks []StateCallback
	for pattern, subs := range sm.subscriptions {
		if sm.matchesPattern(key, pattern) {
			for _, sub := range subs {
				callbacks = append(callbacks, sub.callback)
			}
		}
	}

	sm.mu.Unlock()

	// Call callbacks outside of lock
	for _, callback := range callbacks {
		callback(key, oldValue, nil)
	}
}

// Clear removes all values from state
func (sm *stateManager) Clear() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.state = make(map[string]interface{})
	sm.transaction = nil
}

// BeginTransaction starts a new transaction
func (sm *stateManager) BeginTransaction() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.transaction = &transaction{
		changes:  make(map[string]interface{}),
		original: make(map[string]interface{}),
	}
}

// Commit applies transaction changes
func (sm *stateManager) Commit() {
	sm.mu.Lock()

	if sm.transaction == nil {
		sm.mu.Unlock()
		return
	}

	// Prepare callbacks
	type changeInfo struct {
		key       string
		oldValue  interface{}
		newValue  interface{}
		callbacks []StateCallback
	}
	var changes []changeInfo

	// Apply changes and collect callbacks
	for key, newValue := range sm.transaction.changes {
		oldValue := sm.state[key]

		if newValue == nil {
			// Delete operation
			delete(sm.state, key)
		} else {
			// Set operation
			sm.state[key] = newValue
		}

		// Collect matching callbacks
		var callbacks []StateCallback
		for pattern, subs := range sm.subscriptions {
			if sm.matchesPattern(key, pattern) {
				for _, sub := range subs {
					callbacks = append(callbacks, sub.callback)
				}
			}
		}

		if len(callbacks) > 0 {
			changes = append(changes, changeInfo{
				key:       key,
				oldValue:  oldValue,
				newValue:  newValue,
				callbacks: callbacks,
			})
		}
	}

	// Clear transaction
	sm.transaction = nil
	sm.mu.Unlock()

	// Call callbacks outside of lock
	for _, change := range changes {
		for _, callback := range change.callbacks {
			callback(change.key, change.oldValue, change.newValue)
		}
	}
}

// Rollback discards transaction changes
func (sm *stateManager) Rollback() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.transaction = nil
}

// Subscribe registers a callback for state changes
func (sm *stateManager) Subscribe(pattern string, callback StateCallback) string {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.idCounter++
	id := fmt.Sprintf("sub_%d", sm.idCounter)

	sub := subscription{
		id:       id,
		pattern:  pattern,
		callback: callback,
	}

	sm.subscriptions[pattern] = append(sm.subscriptions[pattern], sub)

	return id
}

// Unsubscribe removes a subscription
func (sm *stateManager) Unsubscribe(subscriptionID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Find and remove subscription
	for pattern, subs := range sm.subscriptions {
		for i, sub := range subs {
			if sub.id == subscriptionID {
				// Remove subscription
				sm.subscriptions[pattern] = append(subs[:i], subs[i+1:]...)

				// Clean up empty patterns
				if len(sm.subscriptions[pattern]) == 0 {
					delete(sm.subscriptions, pattern)
				}
				return
			}
		}
	}
}

// Save persists state to file
func (sm *stateManager) Save(path string) error {
	sm.mu.RLock()
	stateCopy := make(map[string]interface{})
	for k, v := range sm.state {
		stateCopy[k] = v
	}
	sm.mu.RUnlock()

	// Create directory if needed
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Marshal state
	data, err := json.MarshalIndent(stateCopy, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	// Write to file
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write state: %w", err)
	}

	return nil
}

// Load restores state from file
func (sm *stateManager) Load(path string) error {
	// Read file
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read state: %w", err)
	}

	// Unmarshal state
	var newState map[string]interface{}
	if err := json.Unmarshal(data, &newState); err != nil {
		return fmt.Errorf("failed to unmarshal state: %w", err)
	}

	sm.mu.Lock()

	// Collect all changes for callbacks
	type changeInfo struct {
		key       string
		oldValue  interface{}
		newValue  interface{}
		callbacks []StateCallback
	}
	var changes []changeInfo

	// Update state and collect callbacks
	for key, newValue := range newState {
		oldValue := sm.state[key]
		sm.state[key] = newValue

		// Collect matching callbacks
		var callbacks []StateCallback
		for pattern, subs := range sm.subscriptions {
			if sm.matchesPattern(key, pattern) {
				for _, sub := range subs {
					callbacks = append(callbacks, sub.callback)
				}
			}
		}

		if len(callbacks) > 0 {
			changes = append(changes, changeInfo{
				key:       key,
				oldValue:  oldValue,
				newValue:  newValue,
				callbacks: callbacks,
			})
		}
	}

	sm.mu.Unlock()

	// Call callbacks outside of lock
	for _, change := range changes {
		for _, callback := range change.callbacks {
			callback(change.key, change.oldValue, change.newValue)
		}
	}

	return nil
}

// matchesPattern checks if a key matches a subscription pattern
func (sm *stateManager) matchesPattern(key, pattern string) bool {
	// Simple implementation: exact match or prefix match with wildcard
	if pattern == key {
		return true
	}

	// Support patterns like "footer.*" to match "footer.model", "footer.cost", etc.
	if strings.HasSuffix(pattern, ".*") {
		prefix := strings.TrimSuffix(pattern, ".*")
		return strings.HasPrefix(key, prefix+".")
	}

	// Support patterns like "*" to match everything
	if pattern == "*" {
		return true
	}

	return false
}
