package webui

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// TestUserIsolationInServiceMode verifies that events are only forwarded to connections
// with matching user_id in service mode.
func TestUserIsolationInServiceMode(t *testing.T) {
	ws := &ReactWebServer{}

	t.Run("event with user_id forwarded to matching user connection", func(t *testing.T) {
		event := events.UIEvent{
			Type: "some_event",
			Data: map[string]interface{}{
				"user_id": "user-a",
			},
		}
		connInfo := &ConnectionInfo{
			ClientID: "client123",
			UserID:   "user-a",
		}
		got := ws.shouldForwardEventToConnection(event, connInfo)
		if !got {
			t.Error("should forward event with user_id to connection with matching UserID")
		}
	})

	t.Run("event with user_id NOT forwarded to different user connection", func(t *testing.T) {
		event := events.UIEvent{
			Type: "some_event",
			Data: map[string]interface{}{
				"user_id": "user-a",
			},
		}
		connInfo := &ConnectionInfo{
			ClientID: "client123",
			UserID:   "user-b",
		}
		got := ws.shouldForwardEventToConnection(event, connInfo)
		if got {
			t.Error("should NOT forward event with user_id to connection with different UserID")
		}
	})

	t.Run("user-b event forwarded to user-b connection but not user-a", func(t *testing.T) {
		event := events.UIEvent{
			Type: "some_event",
			Data: map[string]interface{}{
				"user_id": "user-b",
			},
		}
		connUserA := &ConnectionInfo{
			ClientID: "client123",
			UserID:   "user-a",
		}
		connUserB := &ConnectionInfo{
			ClientID: "client456",
			UserID:   "user-b",
		}

		gotUserA := ws.shouldForwardEventToConnection(event, connUserA)
		gotUserB := ws.shouldForwardEventToConnection(event, connUserB)

		if gotUserA {
			t.Error("user-b event should NOT forward to user-a connection")
		}
		if !gotUserB {
			t.Error("user-b event should forward to user-b connection")
		}
	})

	t.Run("user_id check happens before client_id check", func(t *testing.T) {
		event := events.UIEvent{
			Type: "some_event",
			Data: map[string]interface{}{
				"user_id":   "user-a",
				"client_id": "client-a",
			},
		}
		connInfo := &ConnectionInfo{
			ClientID: "client-a",
			UserID:   "user-b",
		}
		got := ws.shouldForwardEventToConnection(event, connInfo)
		if got {
			t.Error("user mismatch should block forwarding even when client_id matches")
		}
	})

	t.Run("user_id check happens before chat_id check", func(t *testing.T) {
		event := events.UIEvent{
			Type: "some_event",
			Data: map[string]interface{}{
				"user_id": "user-a",
				"chat_id": "chat-1",
			},
		}
		connInfo := &ConnectionInfo{
			ClientID: "client123",
			ChatID:   "chat-1",
			UserID:   "user-b",
		}
		got := ws.shouldForwardEventToConnection(event, connInfo)
		if got {
			t.Error("user mismatch should block forwarding even when chat_id matches")
		}
	})
}

// TestUserIsolationAllowsEventsWithoutUserID verifies that events without user_id
// are still forwarded for backward compatibility.
func TestUserIsolationAllowsEventsWithoutUserID(t *testing.T) {
	ws := &ReactWebServer{}

	t.Run("event without user_id forwarded to user connection", func(t *testing.T) {
		event := events.UIEvent{
			Type: events.EventTypeMetricsUpdate,
			Data: map[string]interface{}{
				"stats": "some stats",
			},
		}
		connInfo := &ConnectionInfo{
			ClientID: "client123",
			UserID:   "user-a",
		}
		got := ws.shouldForwardEventToConnection(event, connInfo)
		if !got {
			t.Error("should forward event without user_id to connection with UserID (global event)")
		}
	})

	t.Run("global event type without user_id forwarded", func(t *testing.T) {
		event := events.UIEvent{
			Type: events.EventTypeFileContentChanged,
			Data: map[string]interface{}{
				"path": "/path/to/file",
			},
		}
		connInfo := &ConnectionInfo{
			ClientID: "client123",
			UserID:   "user-a",
		}
		got := ws.shouldForwardEventToConnection(event, connInfo)
		if !got {
			t.Error("should forward file_content_changed event without user_id")
		}
	})

	t.Run("security_approval_request without user_id forwarded to user connection", func(t *testing.T) {
		event := events.UIEvent{
			Type: events.EventTypeSecurityApprovalRequest,
			Data: map[string]interface{}{
				"request_id": "req123",
				"tool_name":  "test_tool",
			},
		}
		connInfo := &ConnectionInfo{
			ClientID: "client123",
			UserID:   "user-a",
		}
		got := ws.shouldForwardEventToConnection(event, connInfo)
		if !got {
			t.Error("should forward security_approval_request without user_id (broadcast)")
		}
	})
}

// TestUserIsolationNotAppliedWhenNoUserID verifies that local mode (no UserID on connection)
// doesn't apply user filtering.
func TestUserIsolationNotAppliedWhenNoUserID(t *testing.T) {
	ws := &ReactWebServer{}

	t.Run("event with user_id forwarded to connection without UserID", func(t *testing.T) {
		event := events.UIEvent{
			Type: "some_event",
			Data: map[string]interface{}{
				"user_id":   "user-a",
				"client_id": "client123",
			},
		}
		connInfo := &ConnectionInfo{
			ClientID: "client123",
			UserID:   "", // Local mode - no UserID
		}
		got := ws.shouldForwardEventToConnection(event, connInfo)
		if !got {
			t.Error("in local mode, should forward event even if it has user_id (no user filtering)")
		}
	})

	t.Run("event with client_id forwarded to local mode connection", func(t *testing.T) {
		event := events.UIEvent{
			Type: "some_event",
			Data: map[string]interface{}{
				"client_id": "client123",
			},
		}
		connInfo := &ConnectionInfo{
			ClientID: "client123",
			UserID:   "", // Local mode
		}
		got := ws.shouldForwardEventToConnection(event, connInfo)
		if !got {
			t.Error("should forward event based on client_id in local mode")
		}
	})

	t.Run("global event forwarded to local mode connection", func(t *testing.T) {
		event := events.UIEvent{
			Type: events.EventTypeMetricsUpdate,
			Data: map[string]interface{}{},
		}
		connInfo := &ConnectionInfo{
			ClientID: "client123",
			UserID:   "", // Local mode
		}
		got := ws.shouldForwardEventToConnection(event, connInfo)
		if !got {
			t.Error("should forward global event to local mode connection")
		}
	})
}

// TestUserIDFilteringWithClientIDChatID verifies the interaction between
// user_id, client_id, and chat_id filtering.
func TestUserIDFilteringWithClientIDChatID(t *testing.T) {
	ws := &ReactWebServer{}

	t.Run("all three IDs match - forward", func(t *testing.T) {
		event := events.UIEvent{
			Type: "some_event",
			Data: map[string]interface{}{
				"user_id":   "user-a",
				"client_id": "client-a",
				"chat_id":   "chat-1",
			},
		}
		connInfo := &ConnectionInfo{
			ClientID: "client-a",
			ChatID:   "chat-1",
			UserID:   "user-a",
		}
		got := ws.shouldForwardEventToConnection(event, connInfo)
		if !got {
			t.Error("should forward when user_id, client_id, and chat_id all match")
		}
	})

	t.Run("user_id mismatch blocks even with matching client_id and chat_id", func(t *testing.T) {
		event := events.UIEvent{
			Type: "some_event",
			Data: map[string]interface{}{
				"user_id":   "user-a",
				"client_id": "client-a",
				"chat_id":   "chat-1",
			},
		}
		connInfo := &ConnectionInfo{
			ClientID: "client-a",
			ChatID:   "chat-1",
			UserID:   "user-b",
		}
		got := ws.shouldForwardEventToConnection(event, connInfo)
		if got {
			t.Error("user_id mismatch should block even when client_id and chat_id match")
		}
	})

	t.Run("user_id and client_id match, connection has no chat_id - forward", func(t *testing.T) {
		event := events.UIEvent{
			Type: "some_event",
			Data: map[string]interface{}{
				"user_id":   "user-a",
				"client_id": "client-a",
				"chat_id":   "chat-1",
			},
		}
		connInfo := &ConnectionInfo{
			ClientID: "client-a",
			ChatID:   "", // No chat_id on connection
			UserID:   "user-a",
		}
		got := ws.shouldForwardEventToConnection(event, connInfo)
		if !got {
			t.Error("should forward when user_id and client_id match, even if connection has no chat_id")
		}
	})

	t.Run("event has user_id and client_id, no chat_id - forward", func(t *testing.T) {
		event := events.UIEvent{
			Type: "some_event",
			Data: map[string]interface{}{
				"user_id":   "user-a",
				"client_id": "client-a",
			},
		}
		connInfo := &ConnectionInfo{
			ClientID: "client-a",
			ChatID:   "chat-1",
			UserID:   "user-a",
		}
		got := ws.shouldForwardEventToConnection(event, connInfo)
		if !got {
			t.Error("should forward when user_id and client_id match, event has no chat_id")
		}
	})
}

// TestUserIDFilteringSecurityEvents verifies that security events properly
// respect user boundaries in service mode.
func TestUserIDFilteringSecurityEvents(t *testing.T) {
	ws := &ReactWebServer{}

	t.Run("security_approval_request with user_id - only forwarded to matching user", func(t *testing.T) {
		event := events.UIEvent{
			Type: events.EventTypeSecurityApprovalRequest,
			Data: map[string]interface{}{
				"user_id":    "user-a",
				"request_id": "req123",
				"tool_name":  "test_tool",
			},
		}
		connUserA := &ConnectionInfo{
			ClientID: "client-a",
			UserID:   "user-a",
		}
		connUserB := &ConnectionInfo{
			ClientID: "client-b",
			UserID:   "user-b",
		}

		gotUserA := ws.shouldForwardEventToConnection(event, connUserA)
		gotUserB := ws.shouldForwardEventToConnection(event, connUserB)

		if !gotUserA {
			t.Error("security_approval_request should forward to matching user")
		}
		if gotUserB {
			t.Error("security_approval_request should NOT forward to different user")
		}
	})

	t.Run("security_prompt_request with user_id and client_id", func(t *testing.T) {
		event := events.UIEvent{
			Type: events.EventTypeSecurityPromptRequest,
			Data: map[string]interface{}{
				"user_id":    "user-a",
				"client_id":  "client-a",
				"request_id": "req123",
			},
		}
		connInfo := &ConnectionInfo{
			ClientID: "client-a",
			UserID:   "user-a",
		}
		got := ws.shouldForwardEventToConnection(event, connInfo)
		if !got {
			t.Error("should forward security_prompt_request when user_id and client_id match")
		}
	})

	t.Run("ask_user_request without user_id forwarded to all user connections", func(t *testing.T) {
		event := events.UIEvent{
			Type: events.EventTypeAskUserRequest,
			Data: map[string]interface{}{
				"request_id": "req123",
				"question":   "What should I do?",
			},
		}
		connUserA := &ConnectionInfo{
			ClientID: "client-a",
			UserID:   "user-a",
		}
		connUserB := &ConnectionInfo{
			ClientID: "client-b",
			UserID:   "user-b",
		}

		gotUserA := ws.shouldForwardEventToConnection(event, connUserA)
		gotUserB := ws.shouldForwardEventToConnection(event, connUserB)

		if !gotUserA {
			t.Error("ask_user_request without user_id should forward to user-a")
		}
		if !gotUserB {
			t.Error("ask_user_request without user_id should forward to user-b")
		}
	})
}

// TestUserIDFilteringEdgeCases tests edge cases around user ID filtering.
func TestUserIDFilteringEdgeCases(t *testing.T) {
	ws := &ReactWebServer{}

	t.Run("event with empty user_id string treated as missing", func(t *testing.T) {
		event := events.UIEvent{
			Type: events.EventTypeMetricsUpdate, // Use a global event type
			Data: map[string]interface{}{
				"user_id": "", // Empty string
			},
		}
		connInfo := &ConnectionInfo{
			ClientID: "client123",
			UserID:   "user-a",
		}
		got := ws.shouldForwardEventToConnection(event, connInfo)
		if !got {
			t.Error("event with empty user_id string should be treated as missing user_id (global event)")
		}
	})

	t.Run("event with whitespace-only user_id treated as missing", func(t *testing.T) {
		event := events.UIEvent{
			Type: events.EventTypeMetricsUpdate, // Use a global event type
			Data: map[string]interface{}{
				"user_id": "  ", // Whitespace only
			},
		}
		connInfo := &ConnectionInfo{
			ClientID: "client123",
			UserID:   "user-a",
		}
		got := ws.shouldForwardEventToConnection(event, connInfo)
		if !got {
			t.Error("event with whitespace-only user_id should be treated as missing user_id")
		}
	})

	t.Run("event user_id must match exactly, case-sensitive", func(t *testing.T) {
		event := events.UIEvent{
			Type: "some_event",
			Data: map[string]interface{}{
				"user_id": "User-A", // Mixed case
			},
		}
		connInfo := &ConnectionInfo{
			ClientID: "client123",
			UserID:   "user-a", // Lowercase
		}
		got := ws.shouldForwardEventToConnection(event, connInfo)
		if got {
			t.Error("user_id comparison should be case-sensitive")
		}
	})

	t.Run("event data is not a map - user filtering not applied", func(t *testing.T) {
		event := events.UIEvent{
			Type: "some_event",
			Data: "not a map",
		}
		connInfo := &ConnectionInfo{
			ClientID: "client123",
			UserID:   "user-a",
		}
		got := ws.shouldForwardEventToConnection(event, connInfo)
		if got {
			t.Error("event with non-map data should be rejected before user filtering")
		}
	})

	t.Run("event data is nil - user filtering not applied", func(t *testing.T) {
		event := events.UIEvent{
			Type: "some_event",
			Data: nil,
		}
		connInfo := &ConnectionInfo{
			ClientID: "client123",
			UserID:   "user-a",
		}
		got := ws.shouldForwardEventToConnection(event, connInfo)
		if got {
			t.Error("event with nil data should be rejected before user filtering")
		}
	})
}

// TestUserIDForClient tests the userIDForClient helper method.
func TestUserIDForClient(t *testing.T) {
	ws := &ReactWebServer{
		clientContexts: make(map[string]*webClientContext),
	}

	t.Run("returns empty string for non-existent client", func(t *testing.T) {
		userID := ws.userIDForClient("non-existent")
		if userID != "" {
			t.Errorf("expected empty string for non-existent client, got %q", userID)
		}
	})

	t.Run("returns empty string for empty client ID", func(t *testing.T) {
		userID := ws.userIDForClient("")
		if userID != "" {
			t.Errorf("expected empty string for empty client ID, got %q", userID)
		}
	})

	t.Run("returns UserID from existing client context", func(t *testing.T) {
		ws.clientContexts["client-a"] = &webClientContext{
			UserID: "user-a",
		}
		userID := ws.userIDForClient("client-a")
		if userID != "user-a" {
			t.Errorf("expected user_id %q, got %q", "user-a", userID)
		}
	})

	t.Run("returns empty string when client context has no UserID", func(t *testing.T) {
		ws.clientContexts["client-b"] = &webClientContext{
			UserID: "",
		}
		userID := ws.userIDForClient("client-b")
		if userID != "" {
			t.Errorf("expected empty string, got %q", userID)
		}
	})

	t.Run("trims whitespace from client ID before lookup", func(t *testing.T) {
		ws.clientContexts["client-c"] = &webClientContext{
			UserID: "user-c",
		}
		userID := ws.userIDForClient("  client-c  ")
		if userID != "user-c" {
			t.Errorf("expected user_id %q, got %q", "user-c", userID)
		}
	})

	t.Run("returns empty string when trimmed client ID is empty", func(t *testing.T) {
		userID := ws.userIDForClient("   ")
		if userID != "" {
			t.Errorf("expected empty string for whitespace-only client ID, got %q", userID)
		}
	})
}
