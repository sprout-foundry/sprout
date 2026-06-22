package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// DefaultClarificationTimeout is the default timeout for clarification requests.
const DefaultClarificationTimeout = 60 * time.Second

// ClarificationRequest is the exported representation of a pending clarification request.
type ClarificationRequest struct {
	RequestID  string    `json:"request_id"`
	SubagentID string    `json:"subagent_id"`
	Question   string    `json:"question"`
	CreatedAt  time.Time `json:"created_at"`
}

// ClarificationManager manages pending clarification requests between subagent and parent agents.
// It provides thread-safe tracking of clarification requests and responses via channels.
type ClarificationManager struct {
	mu       sync.RWMutex
	requests map[string]*clarificationEntry
	eventBus *events.EventBus
	timeout  time.Duration
	stopCh   chan struct{}
}

type clarificationEntry struct {
	requestID  string
	subagentID string
	question   string
	responseCh chan string
	createdAt  time.Time
}

// NewClarificationManager creates a manager with default 60s timeout.
func NewClarificationManager(eventBus *events.EventBus) *ClarificationManager {
	return NewClarificationManagerWithTimeout(eventBus, DefaultClarificationTimeout)
}

// NewClarificationManagerWithTimeout creates a manager with a custom timeout.
func NewClarificationManagerWithTimeout(eventBus *events.EventBus, timeout time.Duration) *ClarificationManager {
	cm := &ClarificationManager{
		requests: make(map[string]*clarificationEntry),
		eventBus: eventBus,
		timeout:  timeout,
		stopCh:   make(chan struct{}),
	}
	go cm.cleanupLoop()
	return cm
}

// RequestClarification creates a clarification request, publishes an event, and blocks until a response arrives or timeout.
func (m *ClarificationManager) RequestClarification(ctx context.Context, subagentID, question string) (string, error) {
	// Generate unique request ID
	requestID, err := generateRequestID()
	if err != nil {
		return "", fmt.Errorf("generate request ID: %w", err)
	}

	entry := &clarificationEntry{
		requestID:  requestID,
		subagentID: subagentID,
		question:   question,
		responseCh: make(chan string, 1),
		createdAt:  time.Now(),
	}

	m.mu.Lock()
	m.requests[requestID] = entry
	m.mu.Unlock()

	// Publish event
	m.publishEvent(events.EventTypeDelegateClarificationRequested, subagentID, requestID, question, "")

	// Wait for response with timeout
	select {
	case <-ctx.Done():
		m.mu.Lock()
		delete(m.requests, requestID)
		m.mu.Unlock()
		return "", fmt.Errorf("clarification request cancelled: %w", ctx.Err())
	case <-time.After(m.timeout):
		m.mu.Lock()
		delete(m.requests, requestID)
		m.mu.Unlock()
		return "", fmt.Errorf("clarification request timed out after %s", m.timeout)
	case response := <-entry.responseCh:
		return response, nil
	}
}

// RespondClarification finds a pending request and sends a response to it.
func (m *ClarificationManager) RespondClarification(requestID, response string) error {
	m.mu.Lock()
	entry, ok := m.requests[requestID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("clarification request %q not found", requestID)
	}
	delete(m.requests, requestID)
	m.mu.Unlock()

	// Send response to the waiting channel (non-blocking since we use buffered channel)
	select {
	case entry.responseCh <- response:
	default:
		// Channel already has a value (shouldn't happen, but safe)
	}

	// Publish event
	m.publishEvent(events.EventTypeDelegateClarificationResponded, entry.subagentID, requestID, "", response)

	return nil
}

// GetPendingClarifications returns all pending clarification requests for a subagent.
func (m *ClarificationManager) GetPendingClarifications(subagentID string) []ClarificationRequest {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var pending []ClarificationRequest
	for _, e := range m.requests {
		if subagentID == "" || e.subagentID == subagentID {
			pending = append(pending, ClarificationRequest{
				RequestID:  e.requestID,
				SubagentID: e.subagentID,
				Question:   e.question,
				CreatedAt:  e.createdAt,
			})
		}
	}
	return pending
}

// Cleanup removes expired entries.
func (m *ClarificationManager) Cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for id, e := range m.requests {
		if now.Sub(e.createdAt) > m.timeout {
			delete(m.requests, id)
		}
	}
}

// cleanupLoop runs in a background goroutine to periodically remove expired entries.
func (m *ClarificationManager) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.Cleanup()
		case <-m.stopCh:
			return
		}
	}
}

// Close stops the background cleanup goroutine.
func (m *ClarificationManager) Close() {
	if m.stopCh != nil {
		close(m.stopCh)
	}
}

func (m *ClarificationManager) publishEvent(eventType, subagentID, requestID, question, response string) {
	if m.eventBus == nil {
		return
	}

	data := map[string]interface{}{
		"subagent_id": subagentID,
		"request_id":  requestID,
	}
	if question != "" {
		data["question"] = question
	}
	if response != "" {
		data["response"] = response
	}
	data["timestamp"] = time.Now().UTC().Format(time.RFC3339)

	m.eventBus.Publish(eventType, data)
}

func generateRequestID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("clarify-%d-%s", time.Now().UnixNano(), hex.EncodeToString(b)), nil
}
