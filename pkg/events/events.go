// Package events provides event system for sprout UI architecture
package events

import (
	"fmt"
	"log"
	"sort"
	"sync"
	"sync/atomic"
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
	event          UIEvent
	isCritical     bool
	remainingSubs  *int32
	publishWG      *sync.WaitGroup
}

// isCriticalEvent reports whether eventType must never be silently dropped.
func isCriticalEvent(eventType string) bool {
	switch eventType {
	case EventTypeSecurityApprovalRequest,
		EventTypeSecurityPromptRequest,
		EventTypeAskUserRequest,
		EventTypeEditApprovalRequest,
		EventTypePasswordRequest,
		EventTypeInputRequired:
		return true
	}
	return false
}

// NewEventBus creates a new event bus.
func NewEventBus() *EventBus {
	eb := &EventBus{
		subscribers:   make(map[string]chan UIEvent),
		workers:       make(map[string]*subscriberWorker),
		deliveryQueue: make(chan eventDelivery, sharedDeliveryQueueSize),
	}
	go eb.runDispatcher()
	return eb
}

// Subscribe adds a new subscriber to the event bus and returns its receive
// channel. Each Subscribe spawns a dedicated worker goroutine that reads
// from a private inbox fed by the dispatcher.
func (eb *EventBus) Subscribe(name string) <-chan UIEvent {
	eb.mutex.Lock()
	defer eb.mutex.Unlock()

	// Generous buffer so a transient consumer stall (a backgrounded/laggy
	// browser tab, a burst of token-level stream chunks) doesn't immediately
	// overflow and start silently dropping non-critical events. The
	// dispatcher coalesces adjacent stream chunks before they reach the
	// worker, so this headroom is rarely approached in practice.
	ch := make(chan UIEvent, subscriberBufferSize)
	eb.subscribers[name] = ch
	w := &subscriberWorker{
		inbox: make(chan eventDelivery, subscriberInboxSize),
		done:  make(chan struct{}),
	}
	eb.workers[name] = w
	go eb.runWorker(name, ch, w)
	return ch
}

// Unsubscribe removes a subscriber. The worker is signalled first so it
// can drain its inbox and close the receive channel itself — that avoids
// the close-during-send race that the historical direct-close path could
// hit when a publish was in flight.
//
// Double-Unsubscribe is a no-op. Closing w.done twice would panic, and
// two concurrent Unsubscribe callers race the channel close (one wins,
// one panics). We guard with sync.Once and re-check the map.
func (eb *EventBus) Unsubscribe(name string) {
	eb.mutex.Lock()
	w, wok := eb.workers[name]
	if wok {
		delete(eb.workers, name)
	}
	_, sok := eb.subscribers[name]
	if sok {
		delete(eb.subscribers, name)
	}
	eb.mutex.Unlock()

	if wok {
		w.closeOnce.Do(func() { close(w.done) })
		// The worker closes the receive channel itself when it exits.
		// Don't close it here — concurrent sends from the dispatcher
		// (with a stale snapshot) would panic.
	}
}

// runDispatcher reads coalesced events from deliveryQueue and fans them
// out to each subscriber's inbox. Stream_chunk events are coalesced across
// the queue (not across per-subscriber inboxes), so a burst of 50 token
// chunks becomes 1–3 inbox sends per subscriber instead of 50.
//
// The dispatcher never blocks on a slow subscriber: if a subscriber's
// inbox is full, the dispatcher drops the non-critical event for THAT
// subscriber only and continues. Other subscribers keep receiving.
func (eb *EventBus) runDispatcher() {
	for ev := range eb.deliveryQueue {
		// Coalesce (currently a no-op; see coalesceBatch). Stream-chunk
		// coalescing is performed at the WS write drain in
		// pkg/webui/stream_coalesce.go, where many events have already
		// accumulated — the right place for the optimization.
		batch := eb.coalesceBatch(ev)

		// Snapshot subscribers BY NAME (not by random map iteration
		// order), so we pair each receive channel with its own inbox.
		// Earlier revisions built two parallel slices from `subscribers`
		// and `workers` and paired by index — Go maps iterate in
		// randomized order, so that produced subscriber cross-talk
		// (events meant for A would go to B's receive channel).
		eb.mutex.RLock()
		// Sort names for deterministic snapshot order — also avoids any
		// platform-specific map iteration quirks.
		names := make([]string, 0, len(eb.subscribers))
		for name := range eb.subscribers {
			names = append(names, name)
		}
		sort.Strings(names)
		receives := make([]chan UIEvent, len(names))
		inboxes := make([]chan eventDelivery, len(names))
		hasInbox := make([]bool, len(names))
		for i, name := range names {
			receives[i] = eb.subscribers[name]
			if w, ok := eb.workers[name]; ok {
				inboxes[i] = w.inbox
				hasInbox[i] = true
			}
		}
		eb.mutex.RUnlock()

		for i, name := range names {
			for _, d := range batch {
				if hasInbox[i] {
					inbox := inboxes[i]
					func() {
						defer func() { _ = recover() }()
						select {
						case inbox <- d:
						default:
							if !d.isCritical {
								log.Printf("[EventBus] Dropped %s event: subscriber %s inbox full (cap=%d)", d.event.Type, name, subscriberInboxSize)
								// The event will never reach this subscriber's
								// worker, so account for it here.
								accountForDelivery(d)
								return
							}
							// Critical: drain one and retry once. Bounded
							// latency (no blocking send on the hot path).
							select {
							case <-inbox:
								select {
								case inbox <- d:
								default:
									log.Printf("[EventBus] Dropped critical %s after inbox drain for %s", d.event.Type, name)
									accountForDelivery(d)
								}
							default:
								// Inbox closed (subscriber unsubscribed) or
								// already drained. Account for delivery so
								// Publish can return.
								accountForDelivery(d)
							}
						}
					}()
					continue
				}
				// Bare-channel path: subscriber was added by direct
				// manipulation (test setup or legacy code path), not via
				// Subscribe. forwardToReceive applies the critical
				// drain-replace policy directly to the receive channel.
				func() {
					defer func() { _ = recover() }()
					forwardToReceive(receives[i], d.event)
				}()
				// accountForDelivery here too: there is no worker that
				// will decrement the per-event counter for this subscriber.
				accountForDelivery(d)
			}
		}
	}
}

// accountForDelivery decrements the per-event subscriber counter and
// releases the publishWG when the last subscriber finishes. Safe to call
// from any goroutine. Both fields are nil-guarded for tests that build
// eventDelivery values manually.
func accountForDelivery(d eventDelivery) {
	if d.remainingSubs == nil {
		return
	}
	if atomic.AddInt32(d.remainingSubs, -1) == 0 && d.publishWG != nil {
		d.publishWG.Done()
	}
}

// coalesceBatch is a no-op stub kept so Publish's call site doesn't
// change. The SP-128 macOS freeze was caused by goroutine fan-out, not by
// per-event channel sends, and the worker-pool design eliminates that.
// Coalescing stream chunks is done at the WS writer
// (pkg/webui/stream_coalesce.go) when it drains its receive channel —
// that path coalesces many events that have already accumulated, which
// is the right place for the optimization.
func (eb *EventBus) coalesceBatch(first eventDelivery) []eventDelivery {
	return []eventDelivery{first}
}

// runWorker is the per-subscriber forwarder. It reads events from the
// inbox and pushes them onto the receive channel using the critical
// drain-replace policy. Each subscriber has exactly one of these, started
// at Subscribe time and stopped at Unsubscribe time. After forwarding
// (or deterministically dropping on a full inbox), the worker calls
// accountForDelivery so the originating Publish can return once every
// subscriber has had a chance to process the event.
func (eb *EventBus) runWorker(name string, receive chan UIEvent, w *subscriberWorker) {
	for {
		select {
		case <-w.done:
			// Drain any remaining inbox items, then close receive.
			for {
				select {
				case d := <-w.inbox:
					forwardToReceive(receive, d.event)
					accountForDelivery(d)
				default:
					close(receive)
					return
				}
			}
		case d := <-w.inbox:
			forwardToReceive(receive, d.event)
			accountForDelivery(d)
		}
	}
}

// forwardToReceive applies the critical-event drain-replace policy when
// pushing onto the receive channel. Recovers from send-on-closed-channel
// in case the receive was closed under us (e.g. consumer bailed out).
func forwardToReceive(ch chan UIEvent, ev UIEvent) {
	defer func() { _ = recover() }()
	if isCriticalEvent(ev.Type) {
		select {
		case ch <- ev:
		default:
			select {
			case <-ch:
				select {
				case ch <- ev:
				case <-time.After(1 * time.Second):
					log.Printf("[EventBus] Dropped critical %s event: subscriber unresponsive for 1s after drain", ev.Type)
				}
			default:
			}
		}
		return
	}
	select {
	case ch <- ev:
	default:
		log.Printf("[EventBus] Dropped %s event for slow subscriber (channel full, cap=%d)", ev.Type, subscriberBufferSize)
	}
}

// Publish broadcasts an event to all subscribers.
//
// Publish itself does NO goroutine creation: it queues one eventDelivery
// onto the shared delivery queue (non-blocking for non-critical, brief
// bounded retry for critical) and returns. All fan-out, coalescing, and
// per-subscriber forwarding happens in the persistent dispatcher +
// per-subscriber workers started at bus creation and Subscribe time.
//
// This eliminates the goroutine creation rate that caused the historical
// macOS streaming-freeze: hundreds of token-level chunks/sec no longer
// spawn hundreds of goroutines/sec.
func (eb *EventBus) Publish(eventType string, data any) {
	eb.mutex.Lock()
	eb.nextID++
	event := UIEvent{
		ID:        generateEventID(eb.nextID),
		Type:      eventType,
		Timestamp: time.Now(),
		Data:      data,
	}

	// publishWG preserves the historical synchronous-Publish contract:
	// Publish returns only after delivery has been attempted for every
	// subscriber. The WaitGroup is counted once per Publish, and the
	// LAST worker to process the event (across all subscribers) calls
	// wg.Done() so Publish can return.
	var wg sync.WaitGroup
	wg.Add(1)

	// Snapshot subscriber count under the same lock so the worker's
	// atomic decrement matches the actual fan-out count.
	var remaining int32
	if len(eb.subscribers) > 0 {
		remaining = int32(len(eb.subscribers))
	} else {
		// No subscribers: there's nothing to wait on; we'll call Done
		// below.
	}

	delivery := eventDelivery{
		event:         event,
		isCritical:    isCriticalEvent(eventType),
		remainingSubs: &remaining,
		publishWG:     &wg,
	}
	eb.mutex.Unlock()

	// Edge case: no subscribers. Publish still must return.
	if remaining == 0 {
		wg.Done()
		return
	}

	queued := false
	if delivery.isCritical {
		// Critical: try once, then briefly retry. We don't want to
		// silently lose a security prompt, but we also don't want to
		// block the model goroutine for the full 1-second drain.
		select {
		case eb.deliveryQueue <- delivery:
			queued = true
		default:
			select {
			case eb.deliveryQueue <- delivery:
				queued = true
			case <-time.After(2 * time.Millisecond):
				log.Printf("[EventBus] Dropped critical %s event: delivery queue full", eventType)
			}
		}
	} else {
		select {
		case eb.deliveryQueue <- delivery:
			queued = true
		default:
			log.Printf("[EventBus] Dropped %s event: delivery queue full (cap=%d)", eventType, sharedDeliveryQueueSize)
		}
	}

	if !queued {
		// Drop already accounted for in the log line above; release the
		// wait so Publish returns instead of hanging forever.
		wg.Done()
		return
	}
	wg.Wait()
}

// generateEventID creates a unique event ID
func generateEventID(id int64) string {
	return fmt.Sprintf("%s-%d", time.Now().Format("20060102-150405"), id)
}

// Helper functions for creating specific event types

// QueryStartedEvent creates a query started event
func QueryStartedEvent(query, provider, model string) map[string]interface{} {
	return map[string]interface{}{
		"query":    query,
		"provider": provider,
		"model":    model,
	}
}

// QueryProgressEvent creates a query progress event
func QueryProgressEvent(message string, iteration int, tokensUsed int) map[string]interface{} {
	return map[string]interface{}{
		"message":     message,
		"iteration":   iteration,
		"tokens_used": tokensUsed,
	}
}

// QueryCompletedEvent creates a query completed event
func QueryCompletedEvent(query, response string, tokensUsed int, cost float64, duration time.Duration) map[string]interface{} {
	return map[string]interface{}{
		"query":       query,
		"response":    response,
		"tokens_used": tokensUsed,
		"cost":        cost,
		"duration_ms": duration.Milliseconds(),
	}
}

// ErrorEvent creates an error event
func ErrorEvent(message string, err error) map[string]interface{} {
	data := map[string]interface{}{
		"message": message,
	}
	if err != nil {
		data["error"] = err.Error()
	}
	return data
}

// ToolExecutionEvent creates a tool execution event
func ToolExecutionEvent(toolName, action string, details map[string]interface{}) map[string]interface{} {
	data := map[string]interface{}{
		"tool_name": toolName,
		"action":    action,
	}
	for k, v := range details {
		data[k] = v
	}
	return data
}

// FileChangedEvent creates a file changed event.
//
// The full file content is deliberately NOT transmitted. No consumer reads it —
// the WebUI's handler only uses file_path/action, and the editor refetches a
// file's bytes on demand (and gets disk-change notifications via the lean
// FileContentChangedEvent). Shipping whole-file content here made each event
// large, so a burst (bulk shell edits, many writes) filled the per-subscriber
// channel and the replay ring buffer fast — dropping file_changed events and
// spamming "[EventBus] Dropped file_changed event" logs. The `content` arg is
// retained for call-site compatibility but only its length is surfaced.
func FileChangedEvent(filePath, action string, content string) map[string]interface{} {
	return map[string]interface{}{
		"file_path": filePath,
		"action":    action, // "created", "modified", "deleted", "write", "edit", "git_*", …
		"size":      len(content),
	}
}

// FileContentChangedEvent creates an event indicating a file's content on disk
// has changed while it was open in the editor
func FileContentChangedEvent(filePath string, modTime int64, size int64) map[string]interface{} {
	return map[string]interface{}{
		"file_path": filePath,
		"mod_time":  modTime,
		"size":      size,
	}
}

// PatchConflictInfo holds optional conflict metadata for a workspace_patch event.
type PatchConflictInfo struct {
	Conflict   bool
	TheirsPath string
}

// WorkspacePatchEvent creates a workspace_patch event payload for real-time
// file content synchronization from the agent to the browser.
// The optional conflictInfo parameter enriches the event with conflict
// metadata when the container patch conflicts with unsynced browser edits.
func WorkspacePatchEvent(filePath, content, action string, seqNum int64, conflictInfo ...PatchConflictInfo) map[string]interface{} {
	payload := map[string]interface{}{
		"file_path": filePath,
		"content":   content,
		"action":    action, // "write", "edit"
		"seq":       seqNum,
	}
	if len(conflictInfo) > 0 && conflictInfo[0].Conflict {
		payload["conflict"] = true
		payload["theirs_path"] = conflictInfo[0].TheirsPath
	}
	return payload
}

// StreamChunkEvent creates a stream chunk event with content type
func StreamChunkEvent(chunk string, contentType string) map[string]interface{} {
	return map[string]interface{}{
		"chunk":        chunk,
		"content_type": contentType,
	}
}

// MetricsUpdateEvent creates a metrics update event
func MetricsUpdateEvent(totalTokens, contextTokens, maxContextTokens, iteration int, totalCost float64) map[string]interface{} {
	return map[string]interface{}{
		"total_tokens":       totalTokens,
		"context_tokens":     contextTokens,
		"max_context_tokens": maxContextTokens,
		"iteration":          iteration,
		"total_cost":         totalCost,
	}
}

// MetricsUpdateEventWithCategory is the SP-094-6 variant that
// includes the most-recent error category label so the cost/status
// footer can render "rate-limited, retrying…" distinct from generic
// provider errors. The default MetricsUpdateEvent still exists for
// callers that don't have an error context.
func MetricsUpdateEventWithCategory(totalTokens, contextTokens, maxContextTokens, iteration int, totalCost float64, errorCategory string) map[string]interface{} {
	return map[string]interface{}{
		"total_tokens":       totalTokens,
		"context_tokens":     contextTokens,
		"max_context_tokens": maxContextTokens,
		"iteration":          iteration,
		"total_cost":         totalCost,
		"error_category":     errorCategory,
	}
}

// ValidationEvent creates a validation event
func ValidationEvent(filePath string, diagnostics []map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"file_path":   filePath,
		"diagnostics": diagnostics,
		"timestamp":   time.Now().Format(time.RFC3339),
	}
}

// ToolStartEvent creates a tool start event with rich metadata
func ToolStartEvent(toolName, toolCallID, arguments, displayName, persona string, isSubagent bool, subagentType string, toolIndex int) map[string]interface{} {
	data := map[string]interface{}{
		"tool_name":    toolName,
		"tool_call_id": toolCallID,
		"arguments":    arguments,
		"display_name": displayName,
	}
	if persona != "" {
		data["persona"] = persona
	}
	if isSubagent {
		data["is_subagent"] = true
		if subagentType != "" {
			data["subagent_type"] = subagentType
		}
	}
	data["tool_index"] = toolIndex
	return data
}

// ToolEndEvent creates a tool end event with result and status
func ToolEndEvent(toolCallID, toolName, status, result, errorMessage string, duration time.Duration) map[string]interface{} {
	data := map[string]interface{}{
		"tool_call_id": toolCallID,
		"tool_name":    toolName,
		"status":       status, // "completed" or "failed"
		"duration_ms":  duration.Milliseconds(),
	}
	if result != "" {
		// Truncate results to 2000 chars for the WebUI - full result stays in the conversation
		if len(result) > 2000 {
			data["result"] = result[:2000] + "\n... (truncated)"
			data["result_truncated"] = true
			data["result_length"] = len(result)
		} else {
			data["result"] = result
			data["result_truncated"] = false
			data["result_length"] = len(result)
		}
	}
	if errorMessage != "" {
		data["error"] = errorMessage
	}
	return data
}

// SecurityApprovalRequestEvent creates a security approval request event for the webui
func SecurityApprovalRequestEvent(requestID, toolName, riskLevel, reasoning string, extras map[string]string) map[string]interface{} {
	payload := map[string]interface{}{
		"request_id": requestID,
		"tool_name":  toolName,
		"risk_level": riskLevel,
		"reasoning":  reasoning,
	}
	for k, v := range extras {
		payload[k] = v
	}
	return payload
}

// EditApprovalRequestEvent (SP-072-3) creates an edit_approval_request
// event payload for the per-hunk diff approval gate. requestID uniquely
// identifies the approval so the WebUI can POST a decision back to
// /api/edits/{requestID}/decision. path is the file being edited.
// hunks is a JSON-serializable representation of each diff hunk with
// its line-level change type (context/add/remove). unifiedDiff is the
// raw unified-diff string for display.
func EditApprovalRequestEvent(requestID, path, unifiedDiff string, hunks []map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"request_id":   requestID,
		"file_path":    path,
		"unified_diff": unifiedDiff,
		"hunks":        hunks,
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	}
}

// PasswordRequestEvent (SP-089-3) creates a password_request event payload.
// requestID uniquely identifies the request so the WebUI can POST a
// response to /api/password/{requestID}/respond. command is the shell
// command that triggered the prompt. prompt is the raw prompt text
// detected on the child's stdout/stderr (e.g., "[sudo] password for user:").
func PasswordRequestEvent(requestID, command, prompt string) map[string]interface{} {
	return map[string]interface{}{
		"request_id": requestID,
		"command":    command,
		"prompt":     prompt,
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
	}
}

// TodoUpdateEvent creates a todo update event
func TodoUpdateEvent(todos []map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"todos": todos,
	}
}

// ProviderNoCredentialEvent creates an event signalling that the newly
// active provider requires an API key but doesn't have one configured.
// The frontend uses providerID to drive a toast that opens Settings →
// Credentials scoped to this provider.
func ProviderNoCredentialEvent(providerID, message string) map[string]interface{} {
	return map[string]interface{}{
		"provider": providerID,
		"message":  message,
	}
}

// AgentMessageEvent creates an agent system message event.
// category: "info", "warning", "error", "tool_log", "thought"
func AgentMessageEvent(category, message string, extra map[string]interface{}) map[string]interface{} {
	data := map[string]interface{}{
		"category": category,
		"message":  message,
	}
	for k, v := range extra {
		data[k] = v
	}
	return data
}

// SubagentActivityEvent creates a structured subagent activity event.
// phase is typically "spawn", "output", or "complete".
func SubagentActivityEvent(toolCallID, toolName, phase, message string, details map[string]interface{}) map[string]interface{} {
	data := map[string]interface{}{
		"tool_call_id": toolCallID,
		"tool_name":    toolName,
		"phase":        phase,
		"message":      message,
	}
	for k, v := range details {
		data[k] = v
	}
	return data
}

// SubagentClarificationRequestedEvent creates a delegate_clarification_requested event payload.
func SubagentClarificationRequestedEvent(subagentID, requestID, question string) map[string]interface{} {
	return map[string]interface{}{
		"subagent_id": subagentID,
		"request_id":  requestID,
		"question":    question,
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
	}
}

// SubagentClarificationRespondedEvent creates a delegate_clarification_responded event payload.
func SubagentClarificationRespondedEvent(subagentID, requestID, response string) map[string]interface{} {
	return map[string]interface{}{
		"subagent_id": subagentID,
		"request_id":  requestID,
		"response":    response,
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
	}
}

// WorkspaceChangedEvent creates a workspace changed event
func WorkspaceChangedEvent(daemonRoot, workspaceRoot, previousWorkspaceRoot string) map[string]interface{} {
	return map[string]interface{}{
		"daemon_root":             daemonRoot,
		"workspace_root":          workspaceRoot,
		"previous_workspace_root": previousWorkspaceRoot,
	}
}

// SecurityPromptRequestEvent creates a security prompt request event for the webui
func SecurityPromptRequestEvent(requestID, prompt string, defaultResponse bool, extras map[string]string) map[string]interface{} {
	payload := map[string]interface{}{
		"request_id":       requestID,
		"prompt":           prompt,
		"default_response": defaultResponse,
	}
	for k, v := range extras {
		payload[k] = v
	}
	return payload
}

// SecurityPromptResponseEvent creates a security prompt response event
func SecurityPromptResponseEvent(requestID, response bool) map[string]interface{} {
	return map[string]interface{}{
		"request_id": requestID,
		"response":   response,
	}
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

// AskUserRequestEvent creates an ask_user request event for the webui.
// Accepts any struct whose JSON shape matches AskUserRequest (the
// agent_tools package supplies one). Falls through fields onto the
// flat event payload so existing frontend consumers that only read
// "question" continue to work.
func AskUserRequestEvent(requestID string, req AskUserRequest, clientID string) map[string]interface{} {
	payload := map[string]interface{}{
		"request_id": requestID,
		"question":   req.Question,
	}
	if req.Header != "" {
		payload["header"] = req.Header
	}
	if len(req.Options) > 0 {
		opts := make([]map[string]string, len(req.Options))
		for i, opt := range req.Options {
			entry := map[string]string{"label": opt.Label}
			if opt.Value != "" {
				entry["value"] = opt.Value
			}
			if opt.Description != "" {
				entry["description"] = opt.Description
			}
			opts[i] = entry
		}
		payload["options"] = opts
	}
	if req.MultiSelect {
		payload["multi_select"] = true
	}
	if req.Default != "" {
		payload["default"] = req.Default
	}
	if clientID != "" {
		payload["client_id"] = clientID
	}
	return payload
}

// AskUserCancelledEvent creates a payload for cancelling an in-flight
// ask_user request. The frontend uses the status field to dismiss the
// dialog without losing context — the same status pattern as "responded".
func AskUserCancelledEvent(requestID, clientID string) map[string]interface{} {
	payload := map[string]interface{}{
		"request_id": requestID,
		"status":     "cancelled",
	}
	if clientID != "" {
		payload["client_id"] = clientID
	}
	return payload
}

// InputRequiredEvent creates an input_required event payload.
// reason is a human-readable description of why input is needed
// (e.g., "security_approval", "ask_user", "blocking_prompt").
// requestID optionally links to the specific request event.
func InputRequiredEvent(reason, requestID string) map[string]interface{} {
	payload := map[string]interface{}{
		"reason":    reason,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}
	if requestID != "" {
		payload["request_id"] = requestID
	}
	return payload
}

// CompactStartedEvent creates the payload for a compact_started event.
// source is one of "manual" (slash command) or "auto_llm_summary" (seed
// structural compaction / context-limit recovery). messageCount and
// checkpointCount capture the pre-compact state for diagnostics.
func CompactStartedEvent(source string, messageCount, checkpointCount int) map[string]interface{} {
	return map[string]interface{}{
		"source":           source,
		"message_count":    messageCount,
		"checkpoint_count": checkpointCount,
		"timestamp":        time.Now().UTC().Format(time.RFC3339),
	}
}

// ContextManagementDiagnosticEvent (SP-066 Phase 1, SP-126) reports the
// model-aware context-budget math at a single iteration. Subscribers (WebUI
// metrics panel, telemetry pipelines) use it to verify substitution is doing
// the heavy lifting and the LLM fall-through stays approximately never.
//
// Fields:
//   - current_tokens: tokenizer-estimated size of the prompt going to the model.
//   - max_tokens: the EFFECTIVE max — the smaller of the model's native window
//     and the user's MaxContextTokens cap (SP-126). This is the value seed's
//     budget math operates against. Renamed semantically from the SP-066
//     "hard context-window limit" wording because SP-126 makes the cap a
//     first-class concept; pre-SP-126 the two were identical (no cap).
//   - native_max_tokens: the model's UNCAPPED native window. Equal to
//     max_tokens when no user cap is set; larger than max_tokens when a
//     cap is active. Lets subscribers render "X / 300K of 1M tokens"
//     (effective vs native) distinctly in the metrics panel.
//   - effective_max: max_tokens minus reservation budget; substitution
//     triggers when current_tokens exceeds trigger_fraction × max_tokens.
//   - trigger_fraction: share of max_tokens at which seed triggers compaction
//     (1 − total_reserved_fraction).
//   - reserved_response / reserved_thinking / reserved_tool_io: the three
//     reservation slices as fractions of max_tokens.
//   - iteration: current iteration number from seed's OnIteration callback.
//   - message_count: messages in the prepared prompt list.
//   - cached_tokens: cumulative prompt tokens served from the provider's
//     prompt cache so far this session.
//   - prompt_tokens: cumulative prompt tokens charged so far this session.
//   - cache_write_tokens: cumulative tokens written to the provider's cache
//     (Anthropic cache_create_input_tokens). May be 0 if not tracked.
//   - cache_hit_rate: cached_tokens / prompt_tokens, or 0 when prompt_tokens
//     is 0. Lets the UI render cache effectiveness at a glance.
func ContextManagementDiagnosticEvent(currentTokens, maxTokens, nativeMaxTokens int, triggerFraction, reservedResponse, reservedThinking, reservedToolIO float64, iteration, messageCount int, cachedTokens, promptTokens, cacheWriteTokens int) map[string]interface{} {
	effectiveMax := 0
	if maxTokens > 0 {
		effectiveMax = int(float64(maxTokens) * triggerFraction)
	}
	cacheHitRate := 0.0
	if promptTokens > 0 {
		cacheHitRate = float64(cachedTokens) / float64(promptTokens)
	}
	return map[string]interface{}{
		"current_tokens":     currentTokens,
		"max_tokens":         maxTokens,
		"native_max_tokens":  nativeMaxTokens,
		"effective_max":      effectiveMax,
		"trigger_fraction":   triggerFraction,
		"reserved_response":  reservedResponse,
		"reserved_thinking":  reservedThinking,
		"reserved_tool_io":   reservedToolIO,
		"iteration":          iteration,
		"message_count":      messageCount,
		"cached_tokens":      cachedTokens,
		"prompt_tokens":      promptTokens,
		"cache_write_tokens": cacheWriteTokens,
		"cache_hit_rate":     cacheHitRate,
		"timestamp":          time.Now().UTC().Format(time.RFC3339),
	}
}

// RecallDiagnosticEvent (SP-066 Phase 3) reports a single semantic-recall
// pass. embedDurationMS measures the embed call (the recall query's
// latency on the user's critical path). candidatesConsidered is what the
// store returned before recency rerank + filter. injected/injectedChars
// is what actually landed in the prompt supplement. topScores is the
// raw cosine similarities for the candidates so subscribers can spot
// near-miss patterns and tune the threshold.
func RecallDiagnosticEvent(embedDurationMS float64, candidatesConsidered, injected, injectedChars int, topScores []float32) map[string]interface{} {
	scores := make([]float64, len(topScores))
	for i, s := range topScores {
		scores[i] = float64(s)
	}
	return map[string]interface{}{
		"embed_duration_ms":     embedDurationMS,
		"candidates_considered": candidatesConsidered,
		"injected":              injected,
		"injected_chars":        injectedChars,
		"top_scores":            scores,
		"timestamp":             time.Now().UTC().Format(time.RFC3339),
	}
}

// CompactCompletedEvent creates the payload for a compact_completed event.
// On success, err should be nil and after/summary fields describe the new
// state. On failure, err carries the reason and counts reflect the
// unchanged pre-compact totals.
func CompactCompletedEvent(source string, beforeCount, afterCount int, summaryChars int, err error) map[string]interface{} {
	data := map[string]interface{}{
		"source":               source,
		"before_message_count": beforeCount,
		"after_message_count":  afterCount,
		"summary_chars":        summaryChars,
		"timestamp":            time.Now().UTC().Format(time.RFC3339),
	}
	if err != nil {
		data["error"] = err.Error()
		data["success"] = false
	} else {
		data["success"] = true
	}
	return data
}

// DriftDetectedEvent creates a drift notification event for the WebUI
func DriftDetectedEvent(similarity float64, threshold float64, sessionID string) map[string]interface{} {
	return map[string]interface{}{
		"similarity": similarity,
		"threshold":  threshold,
		"sessionId":  sessionID,
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
		"options":    []string{"continue", "new_chat"},
	}
}

// AutomateSessionStartedEvent creates a session_started event payload.
func AutomateSessionStartedEvent(sessionID, workflow, kind string) map[string]interface{} {
	return map[string]interface{}{
		"session_id": sessionID,
		"workflow":   workflow,
		"kind":       kind,
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
	}
}

// AutomateBudgetUpdateEvent creates a budget_update event payload.
func AutomateBudgetUpdateEvent(sessionID string, spentUSD, budgetUSD float64, fraction float64, iteration int) map[string]interface{} {
	return map[string]interface{}{
		"session_id": sessionID,
		"spent_usd":  spentUSD,
		"budget_usd": budgetUSD,
		"fraction":   fraction,
		"iteration":  iteration,
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
	}
}

// AutomateOutputChunkEvent creates an output_chunk event payload.
// Note: we send chunk_len instead of the full chunk to avoid bloating WS frames.
func AutomateOutputChunkEvent(sessionID string, offset int, chunk string) map[string]interface{} {
	return map[string]interface{}{
		"session_id": sessionID,
		"offset":     offset,
		"chunk_len":  len(chunk),
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
	}
}

// AutomateSessionEndedEvent creates a session_ended event payload.
func AutomateSessionEndedEvent(sessionID, workflow, status string, totalCost float64) map[string]interface{} {
	return map[string]interface{}{
		"session_id": sessionID,
		"workflow":   workflow,
		"status":     status,
		"total_cost": totalCost,
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
	}
}

// OOMWatchdogAlertEvent creates an oom_watchdog_alert event payload.
func OOMWatchdogAlertEvent(nodeCount int, totalRSSBytes uint64, thresholdNodeCount int, thresholdRSSBytes uint64, triggerReason string) map[string]interface{} {
	return map[string]interface{}{
		"node_count":           nodeCount,
		"total_rss_bytes":      totalRSSBytes,
		"threshold_node_count": thresholdNodeCount,
		"threshold_rss_bytes":  thresholdRSSBytes,
		"trigger_reason":       triggerReason,
		"timestamp":            time.Now().UTC().Format(time.RFC3339),
	}
}
