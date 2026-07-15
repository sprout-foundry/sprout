// Package webui provides the React web server with embedded assets.
//
// UserConnections (SP-118 Phase 1) tracks multiple concurrent WebSocket
// connections per user for Mode 2 (daemon / sprout service). Mode 1
// (sprout agent) does not use this type — it continues to use
// activeWSByUserID to enforce single-active-session semantics.
//
// Concurrency: UserConnections uses a sync.RWMutex per user, lazily
// allocated. Read paths (Count, ForEach) take RLock; write paths (Add,
// Remove) take Lock. The user-index map itself is guarded by a single
// sync.RWMutex; the per-user slices (when they exist) are guarded by
// their own sync.RWMutex.
package webui

import (
	"sync"
)

// UserConnection identifies one connected WebSocket inside a
// UserConnections registry. The pointer fields are used as identity
// tokens — Remove matches by pointer equality, not by any string field,
// so callers can hand out the same pointer to a goroutine that may run
// after a removal.
type UserConnection struct {
	Conn      *SafeConn          // shared write mutex for cross-conn notifications
	Raw       interface{}        // underlying *websocket.Conn (kept as interface{} to avoid an import cycle in some test setups)
	SessionID string             // human-readable session id
	ClientID  string             // browser-side client_id (matches ConnectionInfo.ClientID)
	UserID    string             // resolved user id (service mode)
}

// UserConnections holds a registry of WebSocket connections indexed by
// user id (or by client id, in local mode). It supports concurrent
// Add/Remove/Count/ForEach operations.
//
// Zero value is ready to use. No constructor required.
type UserConnections struct {
	mu      sync.RWMutex             // guards byUser
	byUser  map[string]*userConnSlot // userID/ClientID → slot
}

// userConnSlot holds the per-user lock and the live connection slice.
// It is allocated lazily on the first Add for a given user, which keeps
// the zero-config case (no users yet) cheap.
type userConnSlot struct {
	mu    sync.RWMutex
	conns []UserConnection
}

// Add registers a connection under userID. It is safe to call
// concurrently from multiple goroutines. The connection is appended to
// the existing slice (or a new slice if the user has no slot yet).
func (uc *UserConnections) Add(userID string, c UserConnection) {
	if userID == "" {
		// Empty userID would collapse all local-mode connections onto one
		// bucket. Refuse rather than produce surprising global fanout.
		return
	}
	uc.mu.RLock()
	slot, ok := uc.byUser[userID]
	uc.mu.RUnlock()
	if !ok {
		uc.mu.Lock()
		// Re-check under the write lock — another goroutine may have
		// created the slot while we upgraded.
		slot, ok = uc.byUser[userID]
		if !ok {
			slot = &userConnSlot{}
			if uc.byUser == nil {
				uc.byUser = make(map[string]*userConnSlot)
			}
			uc.byUser[userID] = slot
		}
		uc.mu.Unlock()
	}
	slot.mu.Lock()
	slot.conns = append(slot.conns, c)
	slot.mu.Unlock()
}

// Remove unregisters a connection by raw pointer identity. It is safe
// to call concurrently. If the connection is not registered, Remove is
// a no-op. After removal, the slot is left in place even when its slice
// is empty — the per-user lock would otherwise become a churn point
// under high concurrency. Use Count to detect "all gone" if needed.
func (uc *UserConnections) Remove(userID string, raw interface{}) {
	if userID == "" || raw == nil {
		return
	}
	uc.mu.RLock()
	slot, ok := uc.byUser[userID]
	uc.mu.RUnlock()
	if !ok {
		return
	}
	slot.mu.Lock()
	defer slot.mu.Unlock()
	for i, c := range slot.conns {
		if c.Raw == raw {
			// Order-preserving removal. The slice is short (1-few per
			// user in practice), so copy-tail is cheaper than a swap.
			slot.conns = append(slot.conns[:i], slot.conns[i+1:]...)
			return
		}
	}
}

// Count returns the number of registered connections for userID.
// O(1) under the read lock.
func (uc *UserConnections) Count(userID string) int {
	if userID == "" {
		return 0
	}
	uc.mu.RLock()
	slot, ok := uc.byUser[userID]
	uc.mu.RUnlock()
	if !ok {
		return 0
	}
	slot.mu.RLock()
	defer slot.mu.RUnlock()
	return len(slot.conns)
}

// ForEach invokes fn once per connection registered under userID, in
// insertion order. If fn returns false the iteration stops. Safe to
// call concurrently with Add/Remove — fn observes a consistent
// snapshot at the time of the call.
func (uc *UserConnections) ForEach(userID string, fn func(UserConnection) bool) {
	if userID == "" {
		return
	}
	uc.mu.RLock()
	slot, ok := uc.byUser[userID]
	uc.mu.RUnlock()
	if !ok {
		return
	}
	slot.mu.RLock()
	defer slot.mu.RUnlock()
	for _, c := range slot.conns {
		if !fn(c) {
			return
		}
	}
}

// Snapshot returns a copy of the connections registered under userID.
// Used by diagnostics (Phase 5) and by tests that need to assert on
// the live state without holding the slot lock.
func (uc *UserConnections) Snapshot(userID string) []UserConnection {
	if userID == "" {
		return nil
	}
	uc.mu.RLock()
	slot, ok := uc.byUser[userID]
	uc.mu.RUnlock()
	if !ok {
		return nil
	}
	slot.mu.RLock()
	defer slot.mu.RUnlock()
	if len(slot.conns) == 0 {
		return nil
	}
	out := make([]UserConnection, len(slot.conns))
	copy(out, slot.conns)
	return out
}

// AllUserIDs returns the set of user ids that currently have at least
// one registered connection. Order is not specified. The result is a
// snapshot — callers must not mutate it.
func (uc *UserConnections) AllUserIDs() []string {
	uc.mu.RLock()
	defer uc.mu.RUnlock()
	if len(uc.byUser) == 0 {
		return nil
	}
	out := make([]string, 0, len(uc.byUser))
	for k := range uc.byUser {
		out = append(out, k)
	}
	return out
}
