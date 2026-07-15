//go:build !js

// Package webui provides the React web server with embedded assets.
//
// Dead code under SP-118. This file previously implemented the
// multi-device single-active-session registry (mislabeled SP-046-5;
// the archived spec has no §5). The registry is unused —
// multi_device_takeover_test.go has no shared ReactWebServer wiring,
// and there are zero callers. Retained for back-compat with any
// pre-SP-118 callers. Will be removed in a follow-up PR with explicit
// scope (see SP-118-6).
//
// The live path for single-active-session enforcement is the Mode 1
// handler (handleWebSocket_Agent, pkg/webui/websocket_handler.go).
// Multi-session daemon behavior is handled by UserConnections
// (pkg/webui/multi_connection_registry.go).

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
