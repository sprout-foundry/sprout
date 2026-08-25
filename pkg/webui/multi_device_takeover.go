//go:build !js

// Package webui provides the React web server with embedded assets.
//
// ActiveSessionRegistry implements workspace-level multi-device takeover
// for SP-046 sync. It is used by the /api/workspace/takeover endpoint
// (workspace_sync_handlers.go) to track which device holds the active
// editing session for a workspace, enabling coordinated file sync when
// multiple browsers edit the same workspace.
//
// This is separate from the WebSocket connection-level single-active-session
// enforcement, which uses activeWSByUserID (Mode 1, handleWebSocket_Agent)
// or UserConnections (Mode 2, multi_connection_registry.go).

package webui

import "sync"

// ActiveSessionRegistry tracks which device currently holds the active session
// for a given user/session. Only one device may be active at a time; connecting
// from a second device triggers a takeover prompt.
type ActiveSessionRegistry struct {
	mu       sync.RWMutex
	sessions map[string]string // sessionID → active deviceID
}

// NewActiveSessionRegistry creates a new, empty registry.
func NewActiveSessionRegistry() *ActiveSessionRegistry {
	return &ActiveSessionRegistry{
		sessions: make(map[string]string),
	}
}

// RegisterConnection registers (or re-registers) a device for a session.
//
// Returns (takeoverPrompt, existingDeviceID):
//
//   - If the session has no active device, the caller is registered and
//     (false, "") is returned.
//   - If the session already has a different active device, (true, existingDeviceID)
//     is returned so the caller can prompt the user to take over.
//   - If the same device is re-registering, (false, "") is returned (idempotent).
func (r *ActiveSessionRegistry) RegisterConnection(sessionID, deviceID string) (takeoverPrompt bool, existingDeviceID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	active, exists := r.sessions[sessionID]
	if !exists {
		// No active device — register this one.
		r.sessions[sessionID] = deviceID
		return false, ""
	}
	if active == deviceID {
		// Same device re-registering — idempotent, no action needed.
		return false, ""
	}
	// Different device is active — caller should prompt for takeover.
	return true, active
}

// RequestTakeover atomically swaps the active device for a session to
// newDeviceID. Returns the old device ID that should be disconnected,
// or "" if no session was active.
func (r *ActiveSessionRegistry) RequestTakeover(sessionID, newDeviceID string) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	old, exists := r.sessions[sessionID]
	if !exists {
		return ""
	}
	r.sessions[sessionID] = newDeviceID
	return old
}

// GetActiveDevice returns the active device ID for a session, or "" if none.
func (r *ActiveSessionRegistry) GetActiveDevice(sessionID string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.sessions[sessionID]
}

// DisconnectDevice removes a device's session if it matches the currently
// active device. Returns true if the device was removed.
func (r *ActiveSessionRegistry) DisconnectDevice(sessionID, deviceID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	active, exists := r.sessions[sessionID]
	if !exists {
		return false
	}
	if active != deviceID {
		return false
	}
	delete(r.sessions, sessionID)
	return true
}
