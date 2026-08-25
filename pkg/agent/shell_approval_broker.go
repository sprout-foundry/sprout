package agent

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// shellApprovalBroker tracks pending per-part shell approval requests and
// their response channels. It mirrors the passwordPrompterBroker pattern:
// the agent registers a request, blocks on the returned channel, and the
// broker delivers the decisions map when the WebUI user responds (or the
// timeout fires, denying all parts).
//
// Package-level so that any agent instance can resolve any request ID —
// essential in daemon mode where multiple chat agents exist.
var shellApprovalBroker = &shellApprovalBrokerType{
	pending: make(map[string]*shellApprovalEntry),
}

type shellApprovalBrokerType struct {
	mu      sync.Mutex
	pending map[string]*shellApprovalEntry
}

// shellApprovalEntry pairs the response channel with a resolved flag so
// double-respond (e.g. user double-clicking submit) is a safe no-op
// rather than a stale second delivery.
type shellApprovalEntry struct {
	ch       chan map[string]bool
	resolved bool
}

// register creates a buffered response channel for the given request ID and
// returns it. The caller blocks on the channel until decisions are delivered
// or the context/timer fires. cleanup removes the entry after resolution.
func (b *shellApprovalBrokerType) register(requestID string) chan map[string]bool {
	ch := make(chan map[string]bool, 1)
	b.mu.Lock()
	b.pending[requestID] = &shellApprovalEntry{ch: ch}
	b.mu.Unlock()
	return ch
}

// respond delivers the per-part decisions to the waiting goroutine. Returns
// false if the request was not found or already resolved.
func (b *shellApprovalBrokerType) respond(requestID string, decisions map[string]bool) bool {
	b.mu.Lock()
	entry, ok := b.pending[requestID]
	if !ok || entry.resolved {
		b.mu.Unlock()
		return false
	}
	entry.resolved = true
	b.mu.Unlock()
	select {
	case entry.ch <- decisions:
		return true
	default:
		return false
	}
}

// cleanup removes a pending entry after it resolves or times out.
func (b *shellApprovalBrokerType) cleanup(requestID string) {
	b.mu.Lock()
	delete(b.pending, requestID)
	b.mu.Unlock()
}

// generateShellApprovalRequestID produces a unique ID for a shell approval request.
var shellApprovalReqCounter int64

func generateShellApprovalRequestID() string {
	return fmt.Sprintf("shell_%d", atomic.AddInt64(&shellApprovalReqCounter, 1))
}

// RespondToShellApproval delivers per-part decisions for a pending shell
// approval request. Called by the WebUI handler (POST
// /api/shell-approvals/{id}/decision) when the user submits their choices.
// Returns true if the request was found and the decisions were delivered.
func (a *Agent) RespondToShellApproval(requestID string, decisions map[string]bool) bool {
	return shellApprovalBroker.respond(requestID, decisions)
}

// DeliverShellDecision delivers a per-part approval decision to a pending
// shell approval request without requiring an Agent instance. This is used
// by the WASM JS bridge so the webui can resolve shell approval requests in
// cloud mode. Mirrors DeliverEditDecision (edit_approval.go).
func DeliverShellDecision(requestID string, decisions map[string]bool) bool {
	return shellApprovalBroker.respond(requestID, decisions)
}
