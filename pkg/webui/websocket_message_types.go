//go:build !js

package webui

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	maxWebSocketMessageSize = 256 * 1024 // 256KB - application-level limit
	maxMessageTypeLen       = 64
	maxProviderLen          = 64
	maxModelLen             = 128
	maxPersonaLen           = 64
	maxRequestIDLen         = 128
	maxResponseLen          = 4096
	maxEventsCount          = 20
	maxEventNameLen         = 64

	// AllowedMessageTypePing is the "ping" message type
	AllowedMessageTypePing = "ping"
	// AllowedMessageTypePong is the "pong" message type
	AllowedMessageTypePong = "pong"
	// AllowedMessageTypeHeartbeat is the "heartbeat" message type
	AllowedMessageTypeHeartbeat = "heartbeat"
	// AllowedMessageTypeSubscribe is the "subscribe" message type
	AllowedMessageTypeSubscribe = "subscribe"
	// AllowedMessageTypeRequestStats is the "request_stats" message type
	AllowedMessageTypeRequestStats = "request_stats"
	// AllowedMessageTypeProviderChange is the "provider_change" message type
	AllowedMessageTypeProviderChange = "provider_change"
	// AllowedMessageTypeModelChange is the "model_change" message type
	AllowedMessageTypeModelChange = "model_change"
	// AllowedMessageTypePersonaChange is the "persona_change" message type
	AllowedMessageTypePersonaChange = "persona_change"
	// AllowedMessageTypeSecurityApprovalResponse is the "security_approval_response" message type
	AllowedMessageTypeSecurityApprovalResponse = "security_approval_response"
	// AllowedMessageTypeSecurityPromptResponse is the "security_prompt_response" message type
	AllowedMessageTypeSecurityPromptResponse = "security_prompt_response"
	// AllowedMessageTypeAskUserResponse is the "ask_user_response" message type
	AllowedMessageTypeAskUserResponse = "ask_user_response"

	// AllowedMessageTypeSessionTakeover is the "session_takeover" message type (SP-046)
	AllowedMessageTypeSessionTakeover = "session_takeover"

	// AllowedMessageTypeHydrateRequest is the "hydrate_request" message type (SP-046)
	AllowedMessageTypeHydrateRequest = "hydrate_request"

	// AllowedMessageTypeSyncRecover is the "sync_recover" message type (SP-046)
	AllowedMessageTypeSyncRecover = "sync_recover"

	// AllowedMessageTypePause signals the tab is backgrounding but will return —
	// keep any in-flight query running in the background instead of cancelling
	// it on heartbeat staleness.
	AllowedMessageTypePause = "pause"
	// AllowedMessageTypeResume signals the tab is foregrounded again — clear the
	// paused state (a reconnect also clears it implicitly).
	AllowedMessageTypeResume = "resume"
	// AllowedMessageTypeSessionClose signals the tab is closing/navigating away —
	// cancel any in-flight query for this client now rather than waiting out the
	// heartbeat timeout.
	AllowedMessageTypeSessionClose = "session_close"

	// Outbound-only hydration message types (SP-046) — server→client
	AllowedMessageTypeHydrateManifest = "hydrate_manifest"
	AllowedMessageTypeHydrateFile     = "hydrate_file"
	AllowedMessageTypeHydrateComplete = "hydrate_complete"
)

var allowedMessageTypes = map[string]bool{
	AllowedMessageTypePing:                     true,
	AllowedMessageTypePong:                     true,
	AllowedMessageTypeHeartbeat:                true,
	AllowedMessageTypeSubscribe:                true,
	AllowedMessageTypeRequestStats:             true,
	AllowedMessageTypeProviderChange:          true,
	AllowedMessageTypeModelChange:              true,
	AllowedMessageTypePersonaChange:           true,
	AllowedMessageTypeSecurityApprovalResponse: true,
	AllowedMessageTypeSecurityPromptResponse:   true,
	AllowedMessageTypeAskUserResponse:          true,
	AllowedMessageTypeSessionTakeover:          true,
	AllowedMessageTypeHydrateRequest:           true,
	AllowedMessageTypeSyncRecover:              true,
	AllowedMessageTypePause:                    true,
	AllowedMessageTypeResume:                   true,
	AllowedMessageTypeSessionClose:             true,
}

// HydrateManifestData is the data payload for "hydrate_manifest" messages.
// Sent after the workspace scan, before file streaming begins, so the
// client can display a progress bar with ETA.
type HydrateManifestData struct {
	TotalFiles      int64 `json:"total_files"`
	TotalSize       int64 `json:"total_size"`
	EstimateSeconds int64 `json:"estimate_seconds"`
}

// HydrateFileData is the data payload for "hydrate_file" messages.
// Carries a single file's base64-encoded content and metadata.
type HydrateFileData struct {
	Path          string  `json:"path"`
	ContentBase64 string  `json:"content_base64"`
	Size          int64   `json:"size"`
	ModifiedAt    string  `json:"modified_at"`
	ProgressPct   float64 `json:"progress_pct"`
}

// HydrateCompleteData is the data payload for "hydrate_complete" messages.
// Sent after all files have been streamed, summarizing the transfer.
type HydrateCompleteData struct {
	FilesTransferred int64 `json:"files_transferred"`
	TotalBytes       int64 `json:"total_bytes"`
	DurationMs       int64 `json:"duration_ms"`
}

// HydrateRequestData is the data payload for inbound "hydrate_request" messages.
// The body is empty — the client just signals it wants hydration.
type HydrateRequestData struct{}

// Validate performs field-level validation on HydrateRequestData.
// Empty body is always valid — the client just requests hydration.
func (d *HydrateRequestData) Validate() error {
	return nil
}

// WebSocketMessage is the envelope for all incoming WebSocket messages.
// It's used to parse and validate the top-level message structure.
type WebSocketMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

// Validate performs field-level validation on the message.
func (m *WebSocketMessage) Validate() error {
	if m.Type == "" {
		return fmt.Errorf("message type is required")
	}

	m.Type = strings.TrimSpace(m.Type)
	if m.Type == "" {
		return fmt.Errorf("message type cannot be empty")
	}

	if len(m.Type) > maxMessageTypeLen {
		return fmt.Errorf("message type too long: %d characters (max %d)", len(m.Type), maxMessageTypeLen)
	}

	if !allowedMessageTypes[m.Type] {
		return fmt.Errorf("unknown message type: %s", m.Type)
	}

	return nil
}

// SubscribeData is the data payload for "subscribe" messages.
//
// Events is the historical event-type filter (kept for backward compat,
// not currently enforced at the bus level). ChatIDs is the SP-034-3
// addition: registers this connection in the server's chatSubscribers
// list so events targeting any of those chats fan out to this connection
// even when the originating clientID differs (multi-tab consistency).
type SubscribeData struct {
	Events  []string `json:"events"`
	ChatIDs []string `json:"chat_ids,omitempty"`
	Channel string   `json:"channel,omitempty"` // Event channel to opt into (e.g., "automate")
}

// Validate performs field-level validation on SubscribeData.
func (d *SubscribeData) Validate() error {
	if len(d.Events) > maxEventsCount {
		return fmt.Errorf("too many events: %d (max %d)", len(d.Events), maxEventsCount)
	}

	for i, event := range d.Events {
		event = strings.TrimSpace(event)
		if event == "" {
			return fmt.Errorf("event at index %d cannot be empty", i)
		}
		if len(event) > maxEventNameLen {
			return fmt.Errorf("event at index %d too long: %d characters (max %d)", i, len(event), maxEventNameLen)
		}
		d.Events[i] = event
	}

	if len(d.ChatIDs) > maxEventsCount {
		return fmt.Errorf("too many chat_ids: %d (max %d)", len(d.ChatIDs), maxEventsCount)
	}
	for i, chatID := range d.ChatIDs {
		chatID = strings.TrimSpace(chatID)
		if chatID == "" {
			return fmt.Errorf("chat_id at index %d cannot be empty", i)
		}
		if len(chatID) > maxEventNameLen {
			return fmt.Errorf("chat_id at index %d too long: %d characters (max %d)", i, len(chatID), maxEventNameLen)
		}
		d.ChatIDs[i] = chatID
	}

	// Validate optional channel subscription (e.g., "automate").
	d.Channel = strings.TrimSpace(d.Channel)
	if d.Channel != "" {
		if len(d.Channel) > maxEventNameLen {
			return fmt.Errorf("channel too long: %d characters (max %d)", len(d.Channel), maxEventNameLen)
		}
	}

	return nil
}

// ProviderChangeData is the data payload for "provider_change" messages.
type ProviderChangeData struct {
	Provider string `json:"provider"`
}

// Validate performs field-level validation on ProviderChangeData.
func (d *ProviderChangeData) Validate() error {
	d.Provider = strings.TrimSpace(d.Provider)
	if d.Provider == "" {
		return fmt.Errorf("provider is required")
	}
	if len(d.Provider) > maxProviderLen {
		return fmt.Errorf("provider too long: %d characters (max %d)", len(d.Provider), maxProviderLen)
	}
	return nil
}

// ModelChangeData is the data payload for "model_change" messages.
type ModelChangeData struct {
	Model    string `json:"model"`
	Provider string `json:"provider,omitempty"`
}

// Validate performs field-level validation on ModelChangeData.
func (d *ModelChangeData) Validate() error {
	d.Model = strings.TrimSpace(d.Model)
	if d.Model == "" {
		return fmt.Errorf("model is required")
	}
	if len(d.Model) > maxModelLen {
		return fmt.Errorf("model too long: %d characters (max %d)", len(d.Model), maxModelLen)
	}

	if d.Provider != "" {
		d.Provider = strings.TrimSpace(d.Provider)
		if len(d.Provider) > maxProviderLen {
			return fmt.Errorf("provider too long: %d characters (max %d)", len(d.Provider), maxProviderLen)
		}
	}

	return nil
}

// PersonaChangeData is the data payload for "persona_change" messages.
type PersonaChangeData struct {
	Persona string `json:"persona"`
}

// Validate performs field-level validation on PersonaChangeData.
func (d *PersonaChangeData) Validate() error {
	d.Persona = strings.TrimSpace(d.Persona)
	if d.Persona == "" {
		return fmt.Errorf("persona is required")
	}
	if len(d.Persona) > maxPersonaLen {
		return fmt.Errorf("persona too long: %d characters (max %d)", len(d.Persona), maxPersonaLen)
	}
	return nil
}

// SecurityApprovalResponseData is the data payload for "security_approval_response" messages.
//
// Action carries the multi-option dialog choice. Legal values:
//   - ""                      → fall back to Approved bool (legacy clients)
//   - "approve_once"          → equivalent to Approved=true
//   - "approve_always"        → shell-only: approve and persist command to allowlist
//   - "elevate"               → shell-only: approve and bump session risk profile to permissive
//   - "allow_folder_session"  → filesystem-only: approve and allowlist the target folder for this session
//   - "deny"                  → equivalent to Approved=false
//
// Old WebUI clients that only set Approved continue to work because the
// server falls back to bool when Action is empty.
type SecurityApprovalResponseData struct {
	RequestID string `json:"request_id"`
	Approved  bool   `json:"approved"`
	Action    string `json:"action,omitempty"`
}

// Validate performs field-level validation on SecurityApprovalResponseData.
func (d *SecurityApprovalResponseData) Validate() error {
	d.RequestID = strings.TrimSpace(d.RequestID)
	if d.RequestID == "" {
		return fmt.Errorf("request_id is required")
	}
	if len(d.RequestID) > maxRequestIDLen {
		return fmt.Errorf("request_id too long: %d characters (max %d)", len(d.RequestID), maxRequestIDLen)
	}
	d.Action = strings.TrimSpace(d.Action)
	switch d.Action {
	case "", "approve_once", "approve_always", "elevate", "allow_folder_session", "deny":
		// ok
	default:
		return fmt.Errorf("action must be one of: approve_once, approve_always, elevate, allow_folder_session, deny (got %q)", d.Action)
	}
	return nil
}

// SecurityPromptResponseData is the data payload for "security_prompt_response" messages.
type SecurityPromptResponseData struct {
	RequestID string `json:"request_id"`
	Response  bool   `json:"response"`
}

// Validate performs field-level validation on SecurityPromptResponseData.
func (d *SecurityPromptResponseData) Validate() error {
	d.RequestID = strings.TrimSpace(d.RequestID)
	if d.RequestID == "" {
		return fmt.Errorf("request_id is required")
	}
	if len(d.RequestID) > maxRequestIDLen {
		return fmt.Errorf("request_id too long: %d characters (max %d)", len(d.RequestID), maxRequestIDLen)
	}
	return nil
}

// AskUserResponseData is the data payload for "ask_user_response" messages.
type AskUserResponseData struct {
	RequestID string `json:"request_id"`
	Response  string `json:"response"`
}

// Validate performs field-level validation on AskUserResponseData.
func (d *AskUserResponseData) Validate() error {
	d.RequestID = strings.TrimSpace(d.RequestID)
	if d.RequestID == "" {
		return fmt.Errorf("request_id is required")
	}
	if len(d.RequestID) > maxRequestIDLen {
		return fmt.Errorf("request_id too long: %d characters (max %d)", len(d.RequestID), maxRequestIDLen)
	}

	d.Response = strings.TrimSpace(d.Response)
	if d.Response == "" {
		return fmt.Errorf("response cannot be empty")
	}
	if len(d.Response) > maxResponseLen {
		return fmt.Errorf("response too long: %d characters (max %d)", len(d.Response), maxResponseLen)
	}

	return nil
}
