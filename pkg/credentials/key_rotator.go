// Package credentials: the round-robin key-rotation half (split from
// key_pool.go). KeyRotator + its methods (NextKey/Advance/Reset/CurrentIndex),
// the DefaultRotator, NewKeyRotator, and the RotateKey/GetNextKey convenience
// entry points. Pool storage (KeyPool, load/save/add/remove) stays in
// key_pool.go.
package credentials

import (
	"log"
	"sync"
)

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

// NewKeyRotator creates a new KeyRotator instance with initialized state.
// The counters map is created here to ensure it's never nil.
func NewKeyRotator() *KeyRotator {
	return &KeyRotator{
		counters: make(map[string]int),
	}
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
