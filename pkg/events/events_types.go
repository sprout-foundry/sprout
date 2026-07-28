// Package events provides event system for sprout UI architecture
package events

import (
	"sync"
	"time"
)

// UIEvent represents an event that can be forwarded between CLI and Web UI.
//
// @ts-generated  webui/src/types/generated.ts::UIEvent
// SP-034-5b: the EventType* constants below are mirrored as the
// ServerEventType string-literal union in generated.ts. The outbound
// registry in pkg/webui/websocket_outbound_registry.go covers the
// same surface (a test asserts they stay in sync).
type UIEvent struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Data      any       `json:"data"`
}

// Common event types
const (
	EventTypeQueryStarted            = "query_started"
	EventTypeQueryProgress           = "query_progress"
	EventTypeQueryCompleted          = "query_completed"
	EventTypeError                   = "error"
	EventTypeToolExecution           = "tool_execution"
	EventTypeToolStart               = "tool_start"
	EventTypeToolEnd                 = "tool_end"
	EventTypeSubagentActivity        = "subagent_activity"
	EventTypeTodoUpdate              = "todo_update"
	EventTypeFileChanged             = "file_changed"
	EventTypeWorkspacePatch          = "workspace_patch"
	EventTypeFileContentChanged      = "file_content_changed"
	EventTypeStreamChunk             = "stream_chunk"
	EventTypeMetricsUpdate           = "metrics_update"
	EventTypeValidation              = "validation"
	EventTypeSecurityApprovalRequest = "security_approval_request"
	EventTypeSecurityPromptRequest   = "security_prompt_request"
	EventTypeAskUserRequest          = "ask_user_request"
	// EventTypeEditApprovalRequest (SP-072-3) is published when the
	// per-hunk diff approval gate routes a proposed file edit through the
	// WebUI for interactive review. The payload carries the request ID,
	// file path, and the diff hunks with per-line change type so the
	// frontend can render a color-coded review panel with per-hunk
	// accept/reject toggles.
	EventTypeEditApprovalRequest = "edit_approval_request"
	// EventTypeShellApprovalRequest (SP-093-3) is published when a shell
	// command needs per-part approval. The payload carries the request ID,
	// the command, the split parts (with kind/semantic/risk), a unified-view
	// string, and the overall risk level. The WebUI renders a per-part
	// approval panel and POSTs back to /api/shell-approvals/{id}/decision.
	EventTypeShellApprovalRequest = "shell_approval_request"
	// EventTypePasswordRequest (SP-089-3) is published when a shell command
	// needs a password from the user (sudo, passwd, ssh-keygen passphrase,
	// etc.). The payload carries the request ID, the command, and the prompt
	// text detected on the child's stdout. The browser renders a password
	// input and POSTs the response to /api/password/{requestID}/respond.
	EventTypePasswordRequest = "password_request"
	// EventTypeInputRequired is published when the agent is blocked waiting
	// for human input — a security approval, an ask_user prompt, or any
	// other blocking interaction. This is a higher-level signal than the
	// specific security_approval_request / security_prompt_request / ask_user_request
	// events: it lets notification subscribers (CLI bell, browser notification)
	// listen to a single "the agent needs you" signal.
	EventTypeInputRequired = "input_required"
	EventTypeAgentMessage  = "agent_message"
	// EventTypeProviderNoCredential is published when a provider change
	// would activate a provider that requires an API key but doesn't
	// have one configured. The frontend surfaces it as a sticky toast
	// pointing at Settings → Credentials, distinct from generic warning
	// messages that get inlined into the active assistant bubble.
	EventTypeProviderNoCredential = "provider_no_credential"
	EventTypeWorkspaceChanged     = "workspace_changed"
	EventTypeSessionTerminated    = "session_terminated"
	EventTypeDriftDetected        = "drift_detected"
	// EventTypeSessionChanged signals that a chat session's metadata
	// (name, pin state, active state) changed and tabs viewing that chat
	// should reconcile. SP-034-3e.
	EventTypeSessionChanged = "session_changed"
	// EventTypeDelegateClarificationRequested is published when a delegate
	// agent requests clarification from its parent agent.
	EventTypeDelegateClarificationRequested = "delegate_clarification_requested"
	// EventTypeDelegateClarificationResponded is published when a parent
	// agent responds to a delegate's clarification request.
	EventTypeDelegateClarificationResponded = "delegate_clarification_responded"
	// EventTypeCompactStarted fires immediately before a compaction
	// operation begins, whether triggered manually by /compact or
	// automatically by seed's structural compaction / context-limit
	// recovery. The payload's `source` field distinguishes the path.
	EventTypeCompactStarted = "compact_started"
	// EventTypeCompactCompleted fires after the compaction finishes,
	// successful or not. Subscribers (e.g. the auto-transcript snapshot
	// capture) use this to record the post-compact state.
	EventTypeCompactCompleted = "compact_completed"
	// EventTypeContextManagementDiagnostic (SP-066 Phase 1) reports the
	// effective context budget at each iteration so we can verify
	// substitution does the heavy lifting and the LLM fall-through
	// stays near zero.
	EventTypeContextManagementDiagnostic = "context_management_diagnostic"
	// EventTypeRecallDiagnostic (SP-066 Phase 3) reports the per-turn
	// semantic-recall pass: how long the embed took, how many candidates
	// were considered, top scores, and how many items were injected.
	// Subscribers (WebUI metrics panel, eval pipelines) use it to verify
	// recall is surfacing useful matches and to tune the half-life and
	// similarity threshold from real data.
	EventTypeRecallDiagnostic = "recall_diagnostic"

	// EventTypeCommandOutput (SP-114 Phase 2c) is emitted for every chunk
	// of stdout captured from a safe slash command executed via
	// POST /api/command/execute. The chat session's WebSocket subscribers
	// fan out the chunk so the WebUI can stream the output in real time.
	// The HTTP response still returns the aggregated output for
	// non-WebSocket callers (backwards-compatible).
	EventTypeCommandOutput = "command_output"

	// EventTypeCommandOutputDropped (SP-114 Phase 2c) is emitted when
	// the bounded backpressure ring overflows and one or more chunks
	// had to be dropped. The payload's `dropped_bytes` field reports
	// how many bytes of command output were discarded since the last
	// warning. WebUI consumers should display a "some output was
	// dropped" indicator.
	EventTypeCommandOutputDropped = "command_output_dropped"
	// SP-127 Phase 2.7: AllowedPathHit is emitted when a file operation
	// lands under a session-allowlisted folder (workflow-declared allowed_paths
	// OR user clicked "Allow folder this session"). Distinct from the base
	// "allowed" action in the JSONL audit log — this discriminated event
	// lets the WebUI automations panel count per-run folder grants.
	EventTypeAllowedPathHit = "allowed_path_hit"
	// SP-065 Phase 2: Automate session lifecycle events
	EventTypeAutomateSessionStarted = "automate.session_started"
	EventTypeAutomateBudgetUpdate   = "automate.budget_update"
	EventTypeAutomateOutputChunk    = "automate.output_chunk"
	EventTypeAutomateSessionEnded   = "automate.session_ended"
	// EventTypeSSHTunnelStatus signals that an SSH workspace tunnel has
	// changed state — disconnected, reconnecting, or reconnected. Clients
	// use this to show a banner or retry failed requests instead of
	// surfacing raw 502 errors during the reconnect window.
	EventTypeSSHTunnelStatus = "ssh_tunnel_status"
	// EventTypeWorkspaceConflict (SP-046-3) is published when a container
	// patch conflicts with unsynced browser edits. The container writes its
	// version as <path>.theirs instead of overwriting. The payload carries
	// path, theirs_path, hash_container, hash_browser, and modified_at.
	EventTypeWorkspaceConflict = "workspace.conflict_detected"
	// EventTypeWorkspaceHeartbeatLost (SP-046-4) is published when a session's
	// heartbeat has been missed for >60s, indicating the browser tab may have
	// been closed or the connection lost. The container will terminate the
	// running job after this event. The payload carries session_id and
	// last_heartbeat (time.RFC3339).
	EventTypeWorkspaceHeartbeatLost = "workspace.heartbeat_lost"
	// EventTypeWorkspaceSessionMoved (SP-046-5) is published when a user
	// takes over a session on a new device, causing the previous device's
	// WebSocket to be closed. The payload carries session_id and new_device_id.
	// The displaced browser surfaces "This session moved to another device."
	EventTypeWorkspaceSessionMoved = "workspace.session_moved"
	// EventTypeRateLimited is published when a tool or API call hits a
	// rate-limit response from the provider. The payload is a
	// *RateLimitedEvent. WebUI consumes this to show "rate-limited,
	// retrying…" and gate the input.
	EventTypeRateLimited = "rate_limited"
	// EventTypeOOMWatchdogAlert is published by the OOM watchdog when node
	// process count or total RSS exceeds configured thresholds. The payload
	// carries the current counts, thresholds, and which threshold(s) triggered.
	EventTypeOOMWatchdogAlert = "oom_watchdog_alert"
)

// EventBus manages event distribution between CLI and Web UI.
//
// Delivery pipeline (SP-128): Publish -> sharedDeliveryQueue -> dispatcher
// goroutine -> per-subscriber inbox -> per-subscriber worker -> receive
// channel.
//
// Why a queue + workers instead of the historical goroutine-per-publish
// fan-out: a streaming chat produces hundreds of token-level stream_chunk
// events per second, and each publish was spawning one goroutine per
// subscriber. On macOS the scheduler pressure from that churn interacts
// badly with the BSD-derived kernel: the WS write goroutine ends up parked
// in WriteMessage waiting on a full kernel send buffer while Publish keeps
// creating goroutines that can't be scheduled. The model is fine; the
// output pipeline stalls. Coalescing adjacent stream_chunk events at the
// dispatcher (collapsing token bursts into one delivery per worker) and
// giving each subscriber a persistent worker removes the goroutine
// creation rate from the hot path entirely.
//
// Subscribers map retains the historical `map[string]chan UIEvent` shape
// so tests and any downstream code that inspects it see the same channel
// they always have. The worker plumbing (inbox + done) lives in a parallel
// `workers` map keyed by the same name.
type EventBus struct {
	subscribers map[string]chan UIEvent
	workers     map[string]*subscriberWorker
	mutex       sync.RWMutex
	nextID      int64

	deliveryQueue chan eventDelivery
}

// subscriberWorker holds the inbox and stop signal for one subscriber's
// worker goroutine.
//
// closeOnce guards close(done): concurrent Unsubscribe callers, or a
// double-Unsubscribe after a real one, must not double-close the channel
// (closing a closed channel panics).
type subscriberWorker struct {
	inbox     chan eventDelivery
	done      chan struct{}
	closeOnce sync.Once
}

// subscriberBufferSize is the per-subscriber receive channel capacity.
// Non-critical events (e.g. stream_chunk) are dropped when this fills, so
// it's sized to absorb transient backpressure rather than the old 100.
const subscriberBufferSize = 1024

// subscriberInboxSize is the per-subscriber inbox capacity between the
// dispatcher and the worker. The dispatcher does non-blocking sends into
// this; the worker reads from it and feeds the receive channel. Sized
// generously so a momentary worker stall doesn't immediately back-pressure
// the dispatcher and stall coalescing.
const subscriberInboxSize = 64

// sharedDeliveryQueueSize bounds how many coalesced events Publish may
// queue ahead of the dispatcher. Sized for ~tens of seconds of headroom at
// 100 events/sec; if it fills, Publish drops non-critical events and
// short-spins on critical ones.
const sharedDeliveryQueueSize = 4096

// eventDelivery is what the dispatcher hands to each subscriber's worker.
// isCritical is precomputed by Publish so the worker doesn't re-derive it
// per event.
//
// Delivery accounting uses remainingSubs (atomic): Publish sets it to the
// current subscriber count, each worker decrements it after successfully
// forwarding (or deterministically dropping) the event, and the LAST
// worker to decrement calls publishWG.Done() so Publish returns. This
// guarantees Publish returns only after every worker has had a chance to
// process the event — preserving the synchronous-Publish contract that
// tests rely on — without creating one goroutine per Publish.
//
// If deliveryQueue is full and the event is dropped at Publish time,
// remainingSubs stays at zero and Done is called inline so Publish
// returns immediately.
type eventDelivery struct {
	event         UIEvent
	isCritical    bool
	remainingSubs *int32
	publishWG     *sync.WaitGroup
}

// PatchConflictInfo holds optional conflict metadata for a workspace_patch event.
type PatchConflictInfo struct {
	Conflict   bool
	TheirsPath string
}

// AskUserRequest mirrors agent_tools.AskUserRequest in shape; declared
// here to avoid an import cycle (events is a leaf package). The event
// payload carries these fields verbatim so the WebUI can render
// options, header, and the multi-select / default affordances.
type AskUserRequest struct {
	Question    string                 `json:"question"`
	Header      string                 `json:"header,omitempty"`
	Options     []AskUserRequestOption `json:"options,omitempty"`
	MultiSelect bool                   `json:"multi_select,omitempty"`
	Default     string                 `json:"default,omitempty"`
}

// AskUserRequestOption is a single selectable choice in an ask_user prompt.
type AskUserRequestOption struct {
	Label       string `json:"label"`
	Value       string `json:"value,omitempty"`
	Description string `json:"description,omitempty"`
}
