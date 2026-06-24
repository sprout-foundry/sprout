//go:build !js

package webui

import (
	"log"
	"os"
	"strings"
	"sync"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// allowedOutboundMessageTypes is the canonical registry of message
// `type` values the server is allowed to send over a WebSocket. Adding
// a new outbound message type means adding it here AND documenting it
// in docs/WEBUI_PROTOCOL.md (SP-034-6a).
//
// In dev builds (SPROUT_DEV=1), validateOutboundMessageType panics on
// an unknown type so accidental rename/typo regressions are caught
// immediately. In prod it logs + drops — never breaks a live connection
// for an unrecognized payload.
//
// Three categories of outbound message land here:
//   1. Connection control: connection_status, ping, pong, chat_run_restored
//   2. UI events from the EventBus (events.EventType*)
//   3. Per-feature responses: stats_update, error
//
// Sync with pkg/events constants — the bus events all flow through the
// outbound path, so any new EventType must appear in this list too.
var allowedOutboundMessageTypes = map[string]struct{}{
	// Connection control
	"connection_status":          {},
	"ping":                       {},
	"pong":                       {},
	"heartbeat_ack":              {},
	"stats_update":               {},
	// Terminal WebSocket protocol (pkg/webui/terminal_websocket.go).
	// All emitted by the terminal handler and consumed by the React
	// TerminalPane. Forgetting any of these here strands the terminal
	// in "Loading terminal..." or makes it look frozen — the frontend
	// never receives the bytes it's waiting on. Keep this block in
	// sync with the `case` branches in the client-side onmessage
	// handler (services/terminalWebSocket.ts).
	"session_created":            {},
	"session_restored":           {},
	"output":                     {},
	"error_output":               {},
	"pty_exit":                   {},
	"resize_ack":                 {},
	"focus_ack":                  {},
	"blur_ack":                   {},
	wsMessageTypeChatRunRestored: {},      // SP-034-2d
	"connection_state":           {},
	"session_conflict":           {},      // SP-046: sent to new device on conflict
	"session_displaced":          {},      // SP-046: sent to old device being evicted

	// UI events (events.EventType*) — note: events.EventTypeError ==
	// "error", so it's the canonical entry for the error envelope used
	// throughout the codebase. Listed once below.
	events.EventTypeQueryStarted:            {},
	events.EventTypeQueryProgress:           {},
	events.EventTypeQueryCompleted:          {},
	events.EventTypeError:                   {},
	events.EventTypeToolExecution:           {},
	events.EventTypeToolStart:               {},
	events.EventTypeToolEnd:                 {},
	events.EventTypeSubagentActivity:        {},
	events.EventTypeDelegateClarificationRequested: {},
	events.EventTypeDelegateClarificationResponded: {},
	events.EventTypeTodoUpdate:              {},
	events.EventTypeFileChanged:             {},
	events.EventTypeWorkspacePatch:          {},
	events.EventTypeFileContentChanged:      {},
	events.EventTypeStreamChunk:             {},
	events.EventTypeMetricsUpdate:           {},
	events.EventTypeValidation:              {},
	events.EventTypeSecurityApprovalRequest: {},
	events.EventTypeSecurityPromptRequest:   {},
	events.EventTypeAskUserRequest:          {},
	events.EventTypeEditApprovalRequest:     {},
	events.EventTypeInputRequired:           {},
	events.EventTypeAgentMessage:            {},
	events.EventTypeProviderNoCredential:    {},
	events.EventTypeWorkspaceChanged:        {},
	events.EventTypeSessionTerminated:       {},
	events.EventTypeDriftDetected:           {},
	events.EventTypeSessionChanged:          {}, // SP-034-3e
	events.EventTypeCompactStarted:          {},
	events.EventTypeCompactCompleted:        {},

	// Cold hydration (SP-046) — server streams workspace files on first-load
	AllowedMessageTypeHydrateManifest: {},
	AllowedMessageTypeHydrateFile:     {},
	AllowedMessageTypeHydrateComplete: {},

	// Sync recovery (SP-046) — server-side failure recovery paths
	"sync_reconcile":      {},
	"sync_replay_start":   {},
	"sync_replay_file":    {},
	"sync_replay_complete": {},
}

// devModeCached caches the SPROUT_DEV env check so we don't re-parse it
// on every outbound message. sync.Once ensures consistent behavior even
// if SPROUT_DEV is mutated during the process lifetime (we honor whatever
// it was at first check — env changes shouldn't flip dev semantics
// mid-run).
var (
	devModeOnce   sync.Once
	devModeCached bool
)

func isDevBuild() bool {
	devModeOnce.Do(func() {
		v := strings.ToLower(strings.TrimSpace(os.Getenv("SPROUT_DEV")))
		switch v {
		case "1", "true", "yes", "on":
			devModeCached = true
		}
	})
	return devModeCached
}

// validateOutboundMessageType checks whether msgType is in the outbound
// allow-list. Returns true if OK. In dev builds, panics on unknown
// types; in prod, logs and returns false so the caller can drop the
// message rather than send something the frontend doesn't understand.
//
// SP-034-5d: called by every WriteJSON-style outbound point that we
// want to lock down. Hot path — keep the work minimal.
func validateOutboundMessageType(msgType string) bool {
	if msgType == "" {
		// Empty type is always invalid — should never happen in practice.
		log.Printf("webui: outbound message with empty type (dropping)")
		return false
	}
	if _, ok := allowedOutboundMessageTypes[msgType]; ok {
		return true
	}
	if isDevBuild() {
		panic("webui: unknown outbound WebSocket message type: " + msgType +
			" — add it to allowedOutboundMessageTypes in websocket_outbound_registry.go")
	}
	log.Printf("webui: dropping outbound message with unrecognized type %q (add to allowedOutboundMessageTypes if intentional)", msgType)
	return false
}

// RegisterOutboundMessageType lets tests and dynamic features (e.g.
// future plugin event types) add to the allow-list at runtime. Safe to
// call from any goroutine; idempotent. Doesn't take a mutex because the
// map is written at init+test-setup and read on the hot path —
// concurrent reads while writes happen would be unsafe, so the test
// fixture should register BEFORE the WS goroutines start.
func RegisterOutboundMessageType(msgType string) {
	msgType = strings.TrimSpace(msgType)
	if msgType == "" {
		return
	}
	allowedOutboundMessageTypes[msgType] = struct{}{}
}
