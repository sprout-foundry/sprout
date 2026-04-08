// Package credentials provides unified credential resolution for all providers.
// This package contains the single source of truth for environment variable names
// and credential resolution logic, replacing the hardcoded strings previously
// scattered across multiple files.
package credentials

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
)

// KeyPool manages multiple keys for a single provider.
// Keys are stored in order. Rotation state is managed separately by KeyRotator.
type KeyPool struct {
	Keys []string // Ordered list of keys (never nil, may be empty)
}

// KeyRotator manages round-robin rotation across providers.
// It is thread-safe and uses in-memory state (per-process lifetime).
// The rotator tracks which key index should be used next for each provider.
type KeyRotator struct {
	mu       sync.RWMutex
	counters map[string]int // provider -> next index to use
}

// DefaultRotator is the package-level default rotator for use by the
// resolution layer and other components.
var DefaultRotator = NewKeyRotator()

// poolMu protects the Load→modify→Save sequence in AddKeyToPool,
// RemoveKeyFromPool, RemoveKeyFromPoolByIndex, DeleteProviderPool, and
// SaveKeyPool. It prevents concurrent HTTP handlers from losing updates
// due to the read-modify-write TOCTOU race.
var poolMu sync.Mutex

// NewKeyRotator creates a new KeyRotator instance with initialized state.
// The counters map is created here to ensure it's never nil.
func NewKeyRotator() *KeyRotator {
	return &KeyRotator{
		counters: make(map[string]int),
	}
}

// isJSONArrayValue checks if a stored value is a JSON array string.
// Returns false for plain strings (backward compatibility).
func isJSONArrayValue(value string) bool {
	trimmed := strings.TrimSpace(value)
	return strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]")
}

// parseKeyArray parses a stored value into a KeyPool.
// If the value is a JSON array, it parses it as multiple keys.
// If the value is a plain string (not JSON array), it treats it as a single-key pool.
// Returns an empty KeyPool (not error) if the value is empty.
func parseKeyArray(value string) (*KeyPool, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return &KeyPool{Keys: []string{}}, nil
	}

	if isJSONArrayValue(trimmed) {
		var keys []string
		if err := json.Unmarshal([]byte(trimmed), &keys); err != nil {
			// Looks like JSON array but isn't valid — treat as plain string.
			// This handles edge cases like "[some-id]" which matches the
			// bracket heuristic but isn't valid JSON.
			log.Printf("[credentials] Warning: value for provider looks like JSON array but failed to parse, treating as plain string")
			return &KeyPool{Keys: []string{trimmed}}, nil
		}
		// Trim whitespace from each key and filter out empty strings
		filtered := make([]string, 0, len(keys))
		for _, key := range keys {
			if trimmed := strings.TrimSpace(key); trimmed != "" {
				filtered = append(filtered, trimmed)
			}
		}
		keys = filtered
		// Ensure non-nil slice invariant (json.Unmarshal on "[]" returns nil)
		if keys == nil {
			keys = []string{}
		}
		return &KeyPool{Keys: keys}, nil
	}

	// Plain string - treat as single-key pool for backward compatibility
	return &KeyPool{Keys: []string{trimmed}}, nil
}

// serializeKeyArray serializes a KeyPool's keys to a JSON array string.
// Returns an error if marshaling fails.
func serializeKeyArray(keys []string) (string, error) {
	bytes, err := json.Marshal(keys)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// LoadKeyPool loads all keys for a provider from the active backend.
// For file backend, checks if value is JSON array string.
// KeyPoolResult holds the result of loading a key pool, including its source.
type KeyPoolResult struct {
	Pool   *KeyPool
	Source string // Backend source ("keyring", "stored", "" if not found)
}

// MaxPoolEntries is the maximum number of pool entries to probe/cleanup for
// the keyring backend. This is a practical upper bound for pool sizes.
const MaxPoolEntries = 100

// LoadKeyPool loads all keys for a provider from the active backend.
// For file backend, the stored value may be a JSON array or a plain string.
// For keyring backend, probes for provider__pool_N entries after the primary key.
// Returns a pool with 0 keys if no keys are found (no error).
// Returns an error only for backend failures (e.g., config dir inaccessible).
// Falls back to direct Load() if the active backend is unavailable.
func LoadKeyPool(provider string) (*KeyPoolResult, error) {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return &KeyPoolResult{Pool: &KeyPool{Keys: []string{}}, Source: ""}, nil
	}

	// Determine if we're using the keyring backend.
	backend, backendErr := GetStorageBackend()
	isKeyring := backendErr == nil && backend != nil && isKeyringBackend(backend)

	// Try the active backend first
	value, source, err := GetFromActiveBackend(provider)
	if err != nil {
		// Backend unavailable — fall back to direct file store load.
		store, loadErr := Load()
		if loadErr != nil {
			return nil, loadErr
		}
		if raw, ok := store[provider]; ok && strings.TrimSpace(raw) != "" {
			pool, parseErr := parseKeyArray(raw)
			if parseErr != nil {
				return nil, parseErr
			}
			return &KeyPoolResult{Pool: pool, Source: "stored"}, nil
		}
		return &KeyPoolResult{Pool: &KeyPool{Keys: []string{}}, Source: source}, nil
	}

	if value == "" {
		return &KeyPoolResult{Pool: &KeyPool{Keys: []string{}}, Source: source}, nil
	}

	// Keyring backend: probe for provider__pool_N entries to assemble full pool
	if isKeyring {
		keys := []string{value}
		for i := 1; i < MaxPoolEntries; i++ {
			poolKey := fmt.Sprintf("%s__pool_%d", provider, i)
			poolValue, _, probeErr := GetFromActiveBackend(poolKey)
			if probeErr != nil || poolValue == "" {
				break
			}
			keys = append(keys, poolValue)
		}
		return &KeyPoolResult{
			Pool:   &KeyPool{Keys: keys},
			Source: source,
		}, nil
	}

	// File backend: parse as JSON array or plain string
	pool, err := parseKeyArray(value)
	if err != nil {
		return nil, err
	}
	return &KeyPoolResult{Pool: pool, Source: source}, nil
}

// isKeyringBackend returns true if the backend is an OSKeyringBackend.
func isKeyringBackend(backend Backend) bool {
	_, ok := backend.(*OSKeyringBackend)
	return ok
}

// cleanupKeyringPoolEntriesFrom deletes keyring pool_N entries starting at fromIndex.
// It probes provider__pool_fromIndex, provider__pool_fromIndex+1, ... and stops
// as soon as a pool entry is not found (contiguous-key assumption).
func cleanupKeyringPoolEntriesFrom(provider string, fromIndex int) {
	for i := fromIndex; i < MaxPoolEntries; i++ {
		poolKey := fmt.Sprintf("%s__pool_%d", provider, i)
		existing, _, err := GetFromActiveBackend(poolKey)
		if err != nil {
			continue
		}
		if existing != "" {
			if err := DeleteFromActiveBackend(poolKey); err != nil {
				log.Printf("[credentials] Warning: failed to cleanup old pool entry %q: %v", poolKey, err)
			}
		} else {
			break
		}
	}
}

// cleanupKeyringPoolEntries is a convenience wrapper that cleans up all pool_N
// entries (starting from index 1). Used when deleting an entire provider's pool.
func cleanupKeyringPoolEntries(provider string) {
	cleanupKeyringPoolEntriesFrom(provider, 1)
}

// SaveKeyPool saves the key pool to the active backend.
// For file backend, stores as JSON array if len > 1, plain string if len == 1.
// For keyring backend, stores provider (first key), provider__pool_1, etc.
// Cleans up removed pool entries (only for keyring backend).
// Note: The caller must hold poolMu when calling this function.
func SaveKeyPool(provider string, pool *KeyPool) error {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return fmt.Errorf("provider name cannot be empty")
	}
	if pool == nil {
		return fmt.Errorf("key pool cannot be nil")
	}

	backend, err := GetStorageBackend()
	if err != nil {
		return fmt.Errorf("failed to get storage backend: %w", err)
	}

	// Check if using keyring backend
	_, isKeyring := backend.(*OSKeyringBackend)

	if isKeyring {
		// Keyring backend: store each key separately
		if len(pool.Keys) == 0 {
			// Empty pool — clean up any leftover pool_N entries, then delete the main entry
			cleanupKeyringPoolEntries(provider)
			return DeleteFromActiveBackend(provider)
		}

		// Store first key at provider name
		if err := SetToActiveBackend(provider, pool.Keys[0]); err != nil {
			return fmt.Errorf("failed to store primary key for %q: %w", provider, err)
		}

		// Store additional keys at provider__pool_N
		for i := 1; i < len(pool.Keys); i++ {
			poolKey := fmt.Sprintf("%s__pool_%d", provider, i)
			if err := SetToActiveBackend(poolKey, pool.Keys[i]); err != nil {
				return fmt.Errorf("failed to store pool key %d for %q: %w", i, provider, err)
			}
		}

		// Clean up old pool entries that are no longer in the pool
		cleanupKeyringPoolEntriesFrom(provider, len(pool.Keys))

		log.Printf("[credentials] Saved %d keys for provider %q to keyring", len(pool.Keys), provider)
		return nil
	}

	// File backend: store as JSON array or plain string
	var storedValue string
	if len(pool.Keys) <= 1 {
		// Single key or empty - store as plain string for backward compatibility
		if len(pool.Keys) == 1 {
			storedValue = pool.Keys[0]
		}
		// Empty pool - store empty string
	} else {
		// Multiple keys - store as JSON array
		jsonBytes, err := serializeKeyArray(pool.Keys)
		if err != nil {
			return fmt.Errorf("failed to serialize key array: %w", err)
		}
		storedValue = string(jsonBytes)
	}

	if storedValue == "" {
		// Empty pool - delete the entry
		return DeleteFromActiveBackend(provider)
	}

	if err := SetToActiveBackend(provider, storedValue); err != nil {
		return fmt.Errorf("failed to store key pool for %q: %w", provider, err)
	}

	log.Printf("[credentials] Saved %d keys for provider %q to file backend", len(pool.Keys), provider)
	return nil
}

// AddKeyToPool adds a key to a provider's pool.
// Duplicates are rejected (exact string match after trim).
// If the pool was previously a single key (plain string format),
// it will be converted to JSON array format.
func AddKeyToPool(provider, key string) error {
	provider = strings.TrimSpace(provider)
	key = strings.TrimSpace(key)
	if provider == "" {
		return fmt.Errorf("provider name cannot be empty")
	}
	if key == "" {
		return fmt.Errorf("key value cannot be empty")
	}

	poolMu.Lock()

	result, err := LoadKeyPool(provider)
	if err != nil {
		poolMu.Unlock()
		return fmt.Errorf("failed to load key pool for %q: %w", provider, err)
	}

	pool := result.Pool

	// Check for duplicates
	for _, existingKey := range pool.Keys {
		if existingKey == key {
			poolMu.Unlock()
			return fmt.Errorf("key already exists in pool for %q", provider)
		}
	}

	// Add the new key
	pool.Keys = append(pool.Keys, key)

	if err := SaveKeyPool(provider, pool); err != nil {
		poolMu.Unlock()
		return fmt.Errorf("failed to save key pool for %q: %w", provider, err)
	}

	poolMu.Unlock()

	log.Printf("[credentials] Added key to pool for %q (now %d keys)", provider, len(pool.Keys))
	return nil
}

// RemoveKeyFromPool removes a specific key from a provider's pool.
// If only one key remains, it's stored as a plain string (backward compat format).
// Returns an error if the key is not found in the pool.
func RemoveKeyFromPool(provider, key string) error {
	provider = strings.TrimSpace(provider)
	key = strings.TrimSpace(key)
	if provider == "" {
		return fmt.Errorf("provider name cannot be empty")
	}
	if key == "" {
		return fmt.Errorf("key value cannot be empty")
	}

	poolMu.Lock()

	result, err := LoadKeyPool(provider)
	if err != nil {
		poolMu.Unlock()
		return fmt.Errorf("failed to load key pool for %q: %w", provider, err)
	}

	pool := result.Pool

	if len(pool.Keys) == 0 {
		poolMu.Unlock()
		return fmt.Errorf("no keys found in pool for %q", provider)
	}

	// Find and remove the key
	found := false
	newKeys := make([]string, 0, len(pool.Keys))
	for _, k := range pool.Keys {
		if k == key {
			found = true
			continue
		}
		newKeys = append(newKeys, k)
	}

	if !found {
		poolMu.Unlock()
		return fmt.Errorf("key not found in pool for %q", provider)
	}

	pool.Keys = newKeys

	if err := SaveKeyPool(provider, pool); err != nil {
		poolMu.Unlock()
		return fmt.Errorf("failed to save key pool for %q: %w", provider, err)
	}

	poolMu.Unlock()

	log.Printf("[credentials] Removed key from pool for %q (now %d keys)", provider, len(pool.Keys))
	return nil
}

// GetPoolSize returns the number of keys in a provider's pool.
func GetPoolSize(provider string) (int, error) {
	result, err := LoadKeyPool(provider)
	if err != nil {
		return 0, fmt.Errorf("failed to load key pool for %q: %w", provider, err)
	}
	return len(result.Pool.Keys), nil
}

// DeleteProviderPool removes all keys for a provider by saving an empty pool.
// This is the thread-safe way to delete a provider's entire pool — it holds
// poolMu for the full Load→modify→Save sequence. Use this from other packages
// instead of calling SaveKeyPool with an empty pool directly.
func DeleteProviderPool(provider string) error {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return fmt.Errorf("provider name cannot be empty")
	}

	poolMu.Lock()
	defer poolMu.Unlock()

	if err := SaveKeyPool(provider, &KeyPool{Keys: []string{}}); err != nil {
		return fmt.Errorf("failed to delete provider pool for %q: %w", provider, err)
	}

	return nil
}

// RemoveKeyFromPoolByIndex removes the key at the given index from a provider's pool.
// Index is 0-based. This is the safe way to remove keys when the caller only
// has access to masked values (e.g., from a WebUI that displays masked keys).
// Returns an error if the index is out of bounds.
func RemoveKeyFromPoolByIndex(provider string, index int) error {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return fmt.Errorf("provider name cannot be empty")
	}
	if index < 0 {
		return fmt.Errorf("index cannot be negative")
	}

	poolMu.Lock()

	result, err := LoadKeyPool(provider)
	if err != nil {
		poolMu.Unlock()
		return fmt.Errorf("failed to load key pool for %q: %w", provider, err)
	}

	pool := result.Pool

	if index >= len(pool.Keys) {
		poolMu.Unlock()
		return fmt.Errorf("index %d out of bounds (pool has %d keys) for %q", index, len(pool.Keys), provider)
	}

	// Remove key at index
	pool.Keys = append(pool.Keys[:index], pool.Keys[index+1:]...)

	if err := SaveKeyPool(provider, pool); err != nil {
		poolMu.Unlock()
		return fmt.Errorf("failed to save key pool for %q: %w", provider, err)
	}

	poolMu.Unlock()

	log.Printf("[credentials] Removed key at index %d from pool for %q (now %d keys)", index, provider, len(pool.Keys))
	return nil
}

// NextKey returns the next key using round-robin rotation.
// Advances the counter for the provider.
// If pool is empty, returns "".
// If pool has one key, always returns it.
// The rotator state is updated to track the next key to use.
func (r *KeyRotator) NextKey(provider string, pool *KeyPool) string {
	if pool == nil || len(pool.Keys) == 0 {
		return ""
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Normalize counter to pool bounds (handles Advance pushing past pool size)
	if _, exists := r.counters[provider]; !exists {
		r.counters[provider] = 0
	}
	currentIndex := r.counters[provider] % len(pool.Keys)

	key := pool.Keys[currentIndex]

	// Advance counter for next call (round-robin)
	r.counters[provider] = (currentIndex + 1) % len(pool.Keys)

	return key
}

// Advance manually advances the rotation counter by 1 for a provider.
// This is useful when a caller wants to skip a key (e.g., manual rejection).
// The counter is incremented without bounds; NextKey applies modular
// arithmetic when selecting from the pool.
//
// Note: NextKey() also auto-advances the counter after each call, so calling
// Advance immediately after NextKey will skip two positions, not one.
func (r *KeyRotator) Advance(provider string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.counters[provider]; !exists {
		r.counters[provider] = 0
	}

	r.counters[provider]++
	log.Printf("[credentials] Advanced rotation counter for %q to %d", provider, r.counters[provider])
}

// Reset resets the rotation counter for a provider to 0.
// This is useful after a successful key validation or when
// you want to start rotation from the beginning.
func (r *KeyRotator) Reset(provider string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.counters[provider] = 0
	log.Printf("[credentials] Reset rotation counter for %q to 0", provider)
}

// CurrentIndex returns the current rotation index for a provider.
// Returns -1 if the provider has no tracked counter.
func (r *KeyRotator) CurrentIndex(provider string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if idx, exists := r.counters[provider]; exists {
		return idx
	}
	return -1
}

// RotateKey advances the default rotator for a provider by one position.
// Callers can use this to manually skip a key without going through the
// full resolve path. (The rate-limit handler uses RefreshAPIKey instead,
// which resolves and auto-advances via NextKey.)
func RotateKey(provider string) {
	DefaultRotator.Advance(provider)
}

// GetNextKey is a convenience function that gets the next key from the default rotator.
// It loads the pool and returns the next key using round-robin.
func GetNextKey(provider string) (string, error) {
	result, err := LoadKeyPool(provider)
	if err != nil {
		return "", err
	}
	return DefaultRotator.NextKey(provider, result.Pool), nil
}
