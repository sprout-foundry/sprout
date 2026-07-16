//go:build !js

package webui

// Package webui: WebSocket event forwarding and message dispatch (split from websocket_handler.go)

import (
	"log"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

func (ws *ReactWebServer) shouldForwardEventToConnection(event events.UIEvent, connInfo *ConnectionInfo) bool {
	data, _ := event.Data.(map[string]interface{})

	// --- User isolation (service mode) ---
	// If the connection has a UserID, enforce user boundary:
	//   - Events with a user_id must match the connection's UserID
	//   - Events without user_id (or empty/whitespace-only) are allowed through (backward compatible)
	// If the connection has NO UserID (local mode), skip user filtering.
	if connInfo.UserID != "" {
		eventUserID, _ := data["user_id"].(string)
		if strings.TrimSpace(eventUserID) != "" {
			if eventUserID != connInfo.UserID {
				return false
			}
		}
	}

	// SP-065-2e: Automate events require explicit channel subscription.
	// Only forward automate.* events to connections that have opted in
	// via {type: "subscribe", data: {channel: "automate"}}.
	if strings.HasPrefix(event.Type, "automate.") {
		if !connInfo.isSubscribedToChannel("automate") {
			return false
		}
		// Connection has opted in — allow the event. Automate events
		// don't carry client_id/chat_id targeting, so they'd otherwise
		// be rejected by the global event type switch below.
		return true
	}

	// Extract target client_id and chat_id from event
	targetClientID, _ := data["client_id"].(string)
	targetChatID, _ := data["chat_id"].(string)

	// Check if event has client_id targeting
	if strings.TrimSpace(targetClientID) != "" {
		// Event has explicit client_id - must match connection's client_id
		// OR (SP-034-3c) the connection must be subscribed to the event's
		// chat for multi-tab consistency. Security/interaction events still
		// require clientID match because they're authenticating a specific
		// browser session, not broadcasting state.
		if strings.TrimSpace(targetClientID) != strings.TrimSpace(connInfo.ClientID) {
			if isSecurityScopedEvent(event.Type) {
				log.Printf("[SECURITY] Dropping %s event: payload client_id=%q does not match connection client_id=%q (request_id=%v)",
					event.Type, strings.TrimSpace(targetClientID), connInfo.ClientID, data["request_id"])
				return false
			}
			// Allow on multi-tab match: either this connection's primary
			// chat_id matches the event's chat_id, or the connection has
			// explicitly subscribed to the chat via the chatSubscribers
			// registry. Either way, the same chat is open on this tab and
			// the event belongs on its screen.
			targetChat := strings.TrimSpace(targetChatID)
			if targetChat == "" {
				return false // clientID mismatch and no chat scope → drop
			}
			if strings.TrimSpace(connInfo.ChatID) != targetChat &&
				!ws.connectionSubscribedToChat(connInfo, targetChat) {
				return false
			}
			return true
		}
		// Client ID matches, now check chat_id if present
		if strings.TrimSpace(targetChatID) != "" {
			targetChat := strings.TrimSpace(targetChatID)
			// Event has chat_id - connection must match, be unfiltered, or be subscribed.
			// Security-scoped events are strict: they only allow when the connection's
			// primary chat_id matches (or is unfiltered), NOT via chatSubscribers.
			if isSecurityScopedEvent(event.Type) {
				if strings.TrimSpace(connInfo.ChatID) != "" && strings.TrimSpace(connInfo.ChatID) != targetChat {
					return false
				}
			} else {
				// For normal events: allow if connection has no specific chat,
				// its primary chat matches, or it has explicitly subscribed to
				// the target chat (multi-chat switch over persistent WS).
				if strings.TrimSpace(connInfo.ChatID) != "" &&
					strings.TrimSpace(connInfo.ChatID) != targetChat &&
					!ws.connectionSubscribedToChat(connInfo, targetChat) {
					return false
				}
			}
		}
		return true
	}

	// No client_id in event - check chat_id targeting
	if strings.TrimSpace(targetChatID) != "" {
		targetChat := strings.TrimSpace(targetChatID)
		// Event has chat_id but no client_id
		// Forward if connection has matching chat_id, no specific chat, or is subscribed.
		if strings.TrimSpace(connInfo.ChatID) != "" &&
			strings.TrimSpace(connInfo.ChatID) != targetChat &&
			!ws.connectionSubscribedToChat(connInfo, targetChat) {
			return false
		}
		return true
	}

	// No client_id and no chat_id - only allow known global event types
	// or events with user_id (user-scoped broadcasts after passing user filtering above)
	switch event.Type {
	case events.EventTypeMetricsUpdate, events.EventTypeFileContentChanged, events.EventTypeSecurityPromptRequest, events.EventTypeSecurityApprovalRequest, events.EventTypeAskUserRequest, events.EventTypeDriftDetected:
		return true
	default:
		// Allow events with a non-empty user_id (user-scoped broadcasts)
		eventUserID, _ := data["user_id"].(string)
		if strings.TrimSpace(eventUserID) != "" {
			return true
		}
		return false
	}
}

// handleWebSocketMessage processes incoming WebSocket messages.
//
// The `daemon`, `userID`, and `chatID` parameters drive Mode-2-aware
// panic cleanup in safeHandleGoroutine below: a panic in a Mode-2
// message handler must not invalidate sibling windows on the same
// clientID. Tests that pre-date SP-118 pass empty strings / false
// for these and continue to behave as Mode 1.
func (ws *ReactWebServer) handleWebSocketMessage(safeConn *SafeConn, sessionID string, msg *WebSocketMessage, clientID, userID, chatID string, daemon bool) {
	switch msg.Type {
	case AllowedMessageTypePing:
		// Respond to ping with pong
		safeConn.WriteJSON(map[string]interface{}{
			"type": "pong",
			"data": map[string]interface{}{"timestamp": time.Now().Unix()},
		})

	case AllowedMessageTypePong:
		// Client responded to ping - handled by read goroutine timestamp tracking
		// The read goroutine updates lastMessage on any successful read

	case AllowedMessageTypeHeartbeat:
		ws.handleHeartbeatMessage(safeConn, clientID)

	case AllowedMessageTypePause:
		// Tab backgrounded — keep any in-flight query running in the background
		// instead of letting the heartbeat monitor cancel it on staleness.
		log.Printf("[lifecycle] client %s paused (backgrounded) — keeping any active query alive", clientID)
		ws.setClientPaused(clientID, true)

	case AllowedMessageTypeResume:
		// Tab foregrounded — resume normal heartbeat-based cancellation.
		ws.setClientPaused(clientID, false)

	case AllowedMessageTypeSessionClose:
		// Tab closing/navigating away — cancel the in-flight query now rather
		// than waiting out the heartbeat timeout.
		ws.setClientPaused(clientID, false)
		ws.cancelQueryForClient(clientID, "session_closed", "Query cancelled: the Web UI was closed")

	case AllowedMessageTypeSubscribe:
		// Handle subscription requests for specific event types AND
		// (SP-034-3b) chat-id subscriptions for multi-tab consistency.
		data, err := parseAndValidateData[SubscribeData](msg.Data, func(d *SubscribeData) error {
			return d.Validate()
		})
		if err != nil {
			log.Printf("WebSocket %s invalid subscribe data: %v", sessionID, err)
			safeConn.WriteJSON(map[string]interface{}{
				"type": "error",
				"data": map[string]string{"message": err.Error()},
			})
			return
		}
		log.Printf("WebSocket client subscribed to events: %v chat_ids: %v channel: %s", data.Events, data.ChatIDs, data.Channel)

		// Register chat subscriptions so events for these chats fan out
		// to this connection even when the originating clientID differs
		// (e.g. same chat open in two browser tabs).
		if ws.chatSubscribers != nil {
			for _, chatID := range data.ChatIDs {
				ws.chatSubscribers.Subscribe(chatID, safeConn.Conn())
			}
		}

		// SP-065-2e: Register channel subscriptions (e.g., "automate")
		// so automate events are only forwarded to connections that
		// explicitly opted in.
		if data.Channel != "" {
			connInfoVal, ok := ws.connections.Load(safeConn.Conn())
			if ok {
				if ci, ok := connInfoVal.(*ConnectionInfo); ok {
					ci.subscribeToChannel(data.Channel)
				}
			}
		}

	case AllowedMessageTypeRequestStats:
		// Send current stats immediately
		ws.safeHandleGoroutine(safeConn, sessionID, clientID, userID, chatID, daemon, func() {
			stats := ws.gatherStatsForClientID(clientID)
			safeConn.WriteJSON(map[string]interface{}{
				"type": "stats_update",
				"data": stats,
			})
		})

	case AllowedMessageTypeProviderChange:
		data, err := parseAndValidateData[ProviderChangeData](msg.Data, func(d *ProviderChangeData) error {
			return d.Validate()
		})
		if err != nil {
			safeConn.WriteJSON(map[string]interface{}{
				"type": "error",
				"data": map[string]string{"message": err.Error()},
			})
			return
		}
		ws.safeHandleGoroutine(safeConn, sessionID, clientID, userID, chatID, daemon, func() {
			ws.handleProviderChangeMessage(safeConn, data, clientID)
		})

	case AllowedMessageTypeModelChange:
		data, err := parseAndValidateData[ModelChangeData](msg.Data, func(d *ModelChangeData) error {
			return d.Validate()
		})
		if err != nil {
			safeConn.WriteJSON(map[string]interface{}{
				"type": "error",
				"data": map[string]string{"message": err.Error()},
			})
			return
		}
		ws.safeHandleGoroutine(safeConn, sessionID, clientID, userID, chatID, daemon, func() {
			ws.handleModelChangeMessage(safeConn, data, clientID)
		})

	case AllowedMessageTypePersonaChange:
		data, err := parseAndValidateData[PersonaChangeData](msg.Data, func(d *PersonaChangeData) error {
			return d.Validate()
		})
		if err != nil {
			safeConn.WriteJSON(map[string]interface{}{
				"type": "error",
				"data": map[string]string{"message": err.Error()},
			})
			return
		}
		ws.safeHandleGoroutine(safeConn, sessionID, clientID, userID, chatID, daemon, func() {
			ws.handlePersonaChangeMessage(safeConn, data, clientID)
		})

	case AllowedMessageTypeSecurityApprovalResponse:
		data, err := parseAndValidateData[SecurityApprovalResponseData](msg.Data, func(d *SecurityApprovalResponseData) error {
			return d.Validate()
		})
		if err != nil {
			safeConn.WriteJSON(map[string]interface{}{
				"type": "error",
				"data": map[string]string{"message": err.Error()},
			})
			return
		}
		ws.safeHandleGoroutine(safeConn, sessionID, clientID, userID, chatID, daemon, func() {
			ws.handleSecurityApprovalResponse(safeConn, data, clientID)
		})

	case AllowedMessageTypeSecurityPromptResponse:
		data, err := parseAndValidateData[SecurityPromptResponseData](msg.Data, func(d *SecurityPromptResponseData) error {
			return d.Validate()
		})
		if err != nil {
			safeConn.WriteJSON(map[string]interface{}{
				"type": "error",
				"data": map[string]string{"message": err.Error()},
			})
			return
		}
		ws.safeHandleGoroutine(safeConn, sessionID, clientID, userID, chatID, daemon, func() {
			ws.handleSecurityPromptResponse(safeConn, data, clientID)
		})

	case AllowedMessageTypeAskUserResponse:
		data, err := parseAndValidateData[AskUserResponseData](msg.Data, func(d *AskUserResponseData) error {
			return d.Validate()
		})
		if err != nil {
			safeConn.WriteJSON(map[string]interface{}{
				"type": "error",
				"data": map[string]string{"message": err.Error()},
			})
			return
		}
		ws.safeHandleGoroutine(safeConn, sessionID, clientID, userID, chatID, daemon, func() {
			ws.handleAskUserResponse(safeConn, data, clientID)
		})

	case AllowedMessageTypeHydrateRequest:
		// SP-046: client requests cold-hydrate of workspace files.
		// Runs in a goroutine so the read loop stays responsive.
		ws.safeHandleGoroutine(safeConn, sessionID, clientID, userID, chatID, daemon, func() {
			ws.handleColdHydrateRequest(safeConn, ws.getWorkspaceRootForClient(clientID))
		})

	case AllowedMessageTypeSyncRecover:
		// SP-046: client requests sync recovery after container death or browser crash.
		// Runs in a goroutine so the read loop stays responsive.
		ws.safeHandleGoroutine(safeConn, sessionID, clientID, userID, chatID, daemon, func() {
			ws.handleSyncRecoverMessage(safeConn, sessionID, msg, clientID)
		})

	case AllowedMessageTypeSessionTakeover:
		// SP-046: session_takeover is expected only during the conflict
		// wait loop. If it arrives during normal message dispatch, log
		// and ignore — there is nothing to do.
		log.Printf("[SP-118-Mode1] session_takeover received for session %s outside of conflict state, ignoring", sessionID)
	}
}
