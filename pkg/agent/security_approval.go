package agent

import (
	"fmt"
	"strings"
	"sync"

	"github.com/alantheprice/ledit/pkg/events"
)

// SecurityApprovalManager coordinates security approval requests between
// the agent and the webui. It allows the agent to block waiting for a user
// response from the webui when stdin is unavailable.
type SecurityApprovalManager struct {
	mu        sync.Mutex
	pending   map[string]chan bool // requestID -> response channel
}

// NewSecurityApprovalManager creates a new security approval manager.
func NewSecurityApprovalManager() *SecurityApprovalManager {
	return &SecurityApprovalManager{
		pending: make(map[string]chan bool),
	}
}

// nextRequestID generates a unique request ID.
var nextRequestID int64
var requestIDMu sync.Mutex

func generateRequestID() string {
	requestIDMu.Lock()
	defer requestIDMu.Unlock()
	nextRequestID++
	return fmt.Sprintf("sec_%d", nextRequestID)
}

// RequestApproval publishes a security approval event to the event bus and
// blocks until the webui responds with an approval or rejection.
// Returns true if approved, false if rejected.
// If the event bus is nil, returns false (reject for safety).
// Optional extra fields (e.g. "command" for shell_command) can be passed in extra.
func (sam *SecurityApprovalManager) RequestApproval(eventBus *events.EventBus, clientID, toolName, riskLevel, reasoning string, extra map[string]interface{}) bool {
	if eventBus == nil {
		return false
	}

	requestID := generateRequestID()
	responseCh := make(chan bool, 1)

	sam.mu.Lock()
	sam.pending[requestID] = responseCh
	sam.mu.Unlock()

	defer func() {
		sam.mu.Lock()
		delete(sam.pending, requestID)
		sam.mu.Unlock()
	}()

	// Publish the approval request event to the webui
	payload := events.SecurityApprovalRequestEvent(
		requestID, toolName, riskLevel, reasoning,
	)
	if trimmedClientID := strings.TrimSpace(clientID); trimmedClientID != "" {
		payload["client_id"] = trimmedClientID
	}
	// Merge optional extra fields (e.g. the shell command for display in the webui)
	for k, v := range extra {
		payload[k] = v
	}
	eventBus.Publish(events.EventTypeSecurityApprovalRequest, payload)

	// Block waiting for response
	approved, ok := <-responseCh
	if !ok {
		return false // channel closed without response
	}
	return approved
}

// RespondToApproval handles a response from the webui for a pending security request.
// Returns true if the request existed and was responded to, false otherwise.
func (sam *SecurityApprovalManager) RespondToApproval(requestID string, approved bool) bool {
	sam.mu.Lock()
	ch, exists := sam.pending[requestID]
	sam.mu.Unlock()

	if !exists {
		return false
	}

	select {
	case ch <- approved:
		return true
	default:
		return false
	}
}
