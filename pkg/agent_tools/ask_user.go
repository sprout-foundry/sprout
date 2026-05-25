package tools

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/clihooks"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// AskUserManager coordinates ask_user requests between the agent
// and the webui. It follows the same pattern as security.ApprovalManager
// but returns string responses instead of bool.
type AskUserManager struct {
	mu      sync.Mutex
	pending map[string]chan string // requestID -> response channel
	timeout time.Duration
}

const DefaultAskUserTimeout = 10 * time.Minute

var (
	globalAskUserManager   *AskUserManager
	globalAskUserManagerMu sync.RWMutex
)

// SetGlobalAskUserManager sets the global singleton (called by webui setup).
//
// Deprecated: use dependency injection via Agent.InjectWebUIManagers instead.
func SetGlobalAskUserManager(mgr *AskUserManager) {
	globalAskUserManagerMu.Lock()
	globalAskUserManager = mgr
	globalAskUserManagerMu.Unlock()
}

// GetGlobalAskUserManager returns the global singleton.
//
// Deprecated: use dependency injection via Agent.InjectWebUIManagers instead.
func GetGlobalAskUserManager() *AskUserManager {
	globalAskUserManagerMu.RLock()
	defer globalAskUserManagerMu.RUnlock()
	return globalAskUserManager
}

// NewAskUserManager creates a new AskUserManager with the default timeout.
func NewAskUserManager() *AskUserManager {
	return &AskUserManager{
		pending: make(map[string]chan string),
		timeout: DefaultAskUserTimeout,
	}
}

var (
	nextAskReqID   int64
	nextAskReqIDMu sync.Mutex
)

func generateAskUserRequestID() string {
	nextAskReqIDMu.Lock()
	defer nextAskReqIDMu.Unlock()
	nextAskReqID++
	return fmt.Sprintf("ask_%d", nextAskReqID)
}

// RequestAskUser publishes an ask_user_request event and blocks until the
// webui responds, a timeout elapses, the context is cancelled, or the event bus is nil.
// Returns the user's text response.
func (m *AskUserManager) RequestAskUser(ctx context.Context, eventBus *events.EventBus, question, clientID, userID, chatID string) (string, error) {
	if eventBus == nil {
		return "", fmt.Errorf("no event bus available")
	}

	if question == "" {
		return "", fmt.Errorf("empty question provided")
	}

	requestID := generateAskUserRequestID()
	responseCh := make(chan string, 1)

	m.mu.Lock()
	m.pending[requestID] = responseCh
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		delete(m.pending, requestID)
		m.mu.Unlock()
	}()

	payload := events.AskUserRequestEvent(requestID, question, clientID)
	if trimmed := strings.TrimSpace(userID); trimmed != "" {
		payload["user_id"] = trimmed
	}
	if trimmed := strings.TrimSpace(chatID); trimmed != "" {
		payload["chat_id"] = trimmed
	}
	eventBus.Publish(events.EventTypeAskUserRequest, payload)

	timeout := m.timeout
	if timeout <= 0 {
		timeout = DefaultAskUserTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case result, ok := <-responseCh:
		if !ok {
			return "", fmt.Errorf("response channel closed")
		}
		return result, nil
	case <-timer.C:
		log.Printf("Ask user request %s timed out after %v", requestID, timeout)
		return "", fmt.Errorf("user did not respond within %v", timeout)
	case <-ctx.Done():
		log.Printf("Ask user request %s cancelled: %v", requestID, ctx.Err())
		return "", fmt.Errorf("ask_user cancelled: %w", ctx.Err())
	}
}

// RespondToAskUser resolves a pending ask_user request with the user's text response.
// Returns true if the request existed and was responded to, false otherwise.
func (m *AskUserManager) RespondToAskUser(requestID string, response string) bool {
	m.mu.Lock()
	ch, exists := m.pending[requestID]
	m.mu.Unlock()

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

// SetTimeout sets the maximum duration requests will block. A zero or
// negative value resets to the default.
func (m *AskUserManager) SetTimeout(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d <= 0 {
		m.timeout = DefaultAskUserTimeout
	} else {
		m.timeout = d
	}
}

// AskUser prompts the user with a question and reads input from stdin.
// This is the legacy CLI-only implementation kept for backward compatibility.
func AskUser(question string) (string, error) {
	if question == "" {
		return "", fmt.Errorf("empty question provided")
	}
	// SP-048 follow-up: stop any active CLI spinner so it doesn't overwrite
	// the question text on stderr while we render it on stdout.
	clihooks.SuspendIndicator()
	// SP-057 follow-up: pause the SteerInputReader so it releases stdin
	// back to cooked mode. The ask_user tool fires mid-turn, so without
	// this the bufio.Reader below would hit EOF immediately (the steer
	// reader is consuming raw-mode stdin) and the tool would silently
	// return an empty answer.
	clihooks.PauseSteer()
	defer clihooks.ResumeSteer()
	// Display the prompt
	fmt.Printf("%s: ", question)
	// Read user input
	reader := bufio.NewReader(os.Stdin)
	answer, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("read user input: %w", err)
	}
	// Trim whitespace and newline characters
	answer = strings.TrimSpace(answer)
	return answer, nil
}

// AskUserWithEventBus prompts the user with a question using the event bus
// for WebUI mode, falling back to stdin for CLI mode.
func AskUserWithEventBus(ctx context.Context, question string, eventBus *events.EventBus, clientID, userID, chatID string, mgr *AskUserManager) (string, error) {
	if question == "" {
		return "", fmt.Errorf("empty question provided")
	}

	// WebUI mode: route through event bus
	if mgr != nil && eventBus != nil {
		log.Printf("[ask_user] Routing through event bus: clientID=%q chatID=%q", clientID, chatID)
		return mgr.RequestAskUser(ctx, eventBus, question, clientID, userID, chatID)
	}

	if mgr == nil {
		log.Printf("[ask_user] Global AskUserManager is nil — falling back to stdin (WebUI not initialized?)")
	}
	if eventBus == nil {
		log.Printf("[ask_user] Event bus is nil — falling back to stdin")
	}

	// CLI mode: read from stdin
	return AskUser(question)
}
