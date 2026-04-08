package agent

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/alantheprice/ledit/pkg/events"
)

// SecurityApprovalManager coordinates security approval requests between
// the agent and the webui. It allows the agent to block waiting for a user
// response from the webui when stdin is unavailable.
type SecurityApprovalManager struct {
	mu              sync.Mutex
	pending         map[string]chan ApprovalResult // requestID -> response channel
	approvalTimeout time.Duration       // how long to wait for a response before rejecting
}

// DefaultApprovalTimeout is the maximum time RequestApproval will block
// waiting for a webui response. After this duration, the request is
// automatically rejected for safety (the user may have closed the tab).
const DefaultApprovalTimeout = 5 * time.Minute

// NewSecurityApprovalManager creates a new security approval manager with
// the default approval timeout.
func NewSecurityApprovalManager() *SecurityApprovalManager {
	return &SecurityApprovalManager{
		pending:         make(map[string]chan ApprovalResult),
		approvalTimeout: DefaultApprovalTimeout,
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

// ApprovalResult encodes the outcome of a security approval request.
type ApprovalResult int

const (
	// ApprovalRejected means the user explicitly rejected or the request was not
	// delivered (nil event bus, channel closed, or drained by slow consumer).
	ApprovalRejected ApprovalResult = iota
	// ApprovalGranted means the user explicitly approved the request.
	ApprovalGranted
	// ApprovalTimeout means the request timed out waiting for a response.
	ApprovalTimeout
)

func (ar ApprovalResult) String() string {
	switch ar {
	case ApprovalGranted:
		return "granted"
	case ApprovalTimeout:
		return "timed_out"
	default:
		return "rejected"
	}
}

// RequestApproval publishes a security approval event to the event bus and
// blocks until the webui responds with an approval or rejection.
// Returns true if approved, false if rejected.
// If the event bus is nil, returns false (reject for safety).
func (sam *SecurityApprovalManager) RequestApproval(eventBus *events.EventBus, clientID, toolName, riskLevel, reasoning string, extras map[string]string) bool {
	if eventBus == nil {
		return false
	}

	requestID := generateRequestID()
	responseCh := make(chan ApprovalResult, 1)

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
		requestID, toolName, riskLevel, reasoning, extras,
	)
	if trimmedClientID := strings.TrimSpace(clientID); trimmedClientID != "" {
		payload["client_id"] = trimmedClientID
	}
	eventBus.Publish(events.EventTypeSecurityApprovalRequest, payload)

	// Block waiting for response with a timeout to prevent indefinite hangs
	// if the user never responds (e.g., closes the tab, navigates away).
	timeout := sam.approvalTimeout
	if timeout <= 0 {
		timeout = DefaultApprovalTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case result, ok := <-responseCh:
		if !ok {
			return false // channel closed without response
		}
		return result == ApprovalGranted
	case <-timer.C:
		log.Printf("Security approval request %s timed out after %v — rejecting for safety", requestID, timeout)
		return false // timeout — reject for safety
	}
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

	result := ApprovalRejected
	if approved {
		result = ApprovalGranted
	}

	select {
	case ch <- result:
		return true
	default:
		return false
	}
}

// SetApprovalTimeout sets the maximum duration RequestApproval will block
// waiting for a user response. A zero or negative value resets to the default.
func (sam *SecurityApprovalManager) SetApprovalTimeout(d time.Duration) {
	sam.mu.Lock()
	defer sam.mu.Unlock()
	if d <= 0 {
		sam.approvalTimeout = DefaultApprovalTimeout
	} else {
		sam.approvalTimeout = d
	}
}
