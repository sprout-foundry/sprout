package security

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/alantheprice/ledit/pkg/events"
)

// SecurityPromptManager coordinates security prompt requests between
// the agent and the webui. It allows the agent to block waiting for a user
// response from the webui when stdin is unavailable.
type SecurityPromptManager struct {
	mu              sync.Mutex
	pending         map[string]chan bool // requestID -> response channel
	approvalTimeout time.Duration
}

// DefaultPromptTimeout is the maximum time RequestPrompt will block
// waiting for a webui response.
const DefaultPromptTimeout = 5 * time.Minute

// nextRequestID generates a unique request ID.
var (
	nextRequestID int64
	requestIDMu   sync.Mutex
)

// globalPromptManager is the global instance of SecurityPromptManager used for WebUI mode
var globalPromptManager *SecurityPromptManager
var globalPromptManagerMu sync.RWMutex

// NewSecurityPromptManager creates a new security prompt manager.
func NewSecurityPromptManager() *SecurityPromptManager {
	return &SecurityPromptManager{
		pending:         make(map[string]chan bool),
		approvalTimeout: DefaultPromptTimeout,
	}
}

// SetGlobalPromptManager sets the global prompt manager instance (called by webui)
func SetGlobalPromptManager(mgr *SecurityPromptManager) {
	globalPromptManagerMu.Lock()
	globalPromptManager = mgr
	globalPromptManagerMu.Unlock()
}

// GetGlobalPromptManager returns the global prompt manager instance
func GetGlobalPromptManager() *SecurityPromptManager {
	globalPromptManagerMu.RLock()
	defer globalPromptManagerMu.RUnlock()
	return globalPromptManager
}

func generateRequestID() string {
	requestIDMu.Lock()
	defer requestIDMu.Unlock()
	nextRequestID++
	return fmt.Sprintf("sec_prompt_%d", nextRequestID)
}

// RequestPrompt publishes a security prompt request event to the event bus and
// blocks until the webui responds with a 'yes' or 'no' response.
// Returns true for 'yes' and false for 'no'.
// If the event bus is nil or stdin is unavailable, returns the default response.
func (spm *SecurityPromptManager) RequestPrompt(eventBus *events.EventBus, prompt string, defaultResponse bool, extras map[string]string) bool {
	if eventBus == nil {
		return defaultResponse
	}

	requestID := generateRequestID()
	responseCh := make(chan bool, 1)

	spm.mu.Lock()
	spm.pending[requestID] = responseCh
	spm.mu.Unlock()

	defer func() {
		spm.mu.Lock()
		delete(spm.pending, requestID)
		spm.mu.Unlock()
	}()

	// Publish the prompt request event to the webui
	payload := events.SecurityPromptRequestEvent(requestID, prompt, defaultResponse, extras)
	eventBus.Publish(events.EventTypeSecurityPromptRequest, payload)

	// Block waiting for response with a timeout to prevent indefinite hangs
	// if the user never responds (e.g., closes the tab, navigates away).
	timeout := spm.approvalTimeout
	if timeout <= 0 {
		timeout = DefaultPromptTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case response, ok := <-responseCh:
		if !ok {
			return defaultResponse // channel closed without response
		}
		return response
	case <-timer.C:
		log.Printf("Security prompt request %s timed out after %v — returning default response", requestID, timeout)
		return defaultResponse // timeout — return the safe default
	}
}

// RespondToPrompt handles a response from the webui for a pending security prompt.
// Returns true if the request existed and was responded to, false otherwise.
func (spm *SecurityPromptManager) RespondToPrompt(requestID string, response bool) bool {
	spm.mu.Lock()
	ch, exists := spm.pending[requestID]
	spm.mu.Unlock()

	if !exists {
		return false
	}

	select {
	case ch <- response:
		return true
	default:
		return false
	}
}

// SetPromptTimeout sets the maximum duration RequestPrompt will block
// waiting for a user response. A zero or negative value resets to the default.
func (spm *SecurityPromptManager) SetPromptTimeout(d time.Duration) {
	spm.mu.Lock()
	defer spm.mu.Unlock()
	if d <= 0 {
		spm.approvalTimeout = DefaultPromptTimeout
	} else {
		spm.approvalTimeout = d
	}
}
