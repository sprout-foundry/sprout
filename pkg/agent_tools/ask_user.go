package tools

// ask_user.go — the ask-user core: the AskUserOption / AskUserRequest
// types, the AskUserManager + the global manager, the event-bus request /
// response path (RequestAskUser, RespondToAskUser, respondSensitive, SetTimeout,
// AskUserWithEventBus), the main AskUser dispatcher, and the option-resolution
// helpers (toEventRequest, optionValue). The CLI rendering + interactive input
// helpers live in ask_user_cli.go.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/clihooks"
	"github.com/sprout-foundry/sprout/pkg/credentials"
	"github.com/sprout-foundry/sprout/pkg/events"

	"golang.org/x/term"
)

// AskUserOption is a single selectable choice in a structured ask_user
// request. When Value is empty the response carries Label verbatim.
type AskUserOption struct {
	Label       string `json:"label"`
	Value       string `json:"value,omitempty"`
	Description string `json:"description,omitempty"`
}

// AskUserRequest carries the full prompt payload from the tool layer to
// the CLI / WebUI renderer. Only Question is required.
type AskUserRequest struct {
	Question    string          `json:"question"`
	Header      string          `json:"header,omitempty"`
	Options     []AskUserOption `json:"options,omitempty"`
	MultiSelect bool            `json:"multi_select,omitempty"`
	Default     string          `json:"default,omitempty"`
	// Sensitive marks the response as a credential: the WebUI renders a
	// masked (password-type) input, the CLI reads without echo, and the
	// backend diverts the response into the credential store instead of
	// returning it to the model. Sensitive requests MUST carry CredentialKey.
	Sensitive bool `json:"sensitive,omitempty"`
	// CredentialKey is the credential-store key the response is written to
	// (e.g. "mcp/figma/FIGMA_TOKEN"). Required when Sensitive is true. It is
	// not a secret — the dialog shows it so the user knows where the value
	// lands.
	CredentialKey string `json:"credential_key,omitempty"`
}

// ErrAskUserNoChannel is returned when no input channel is available
// (no WebUI client, stdin not a TTY / closed). The LLM should treat
// this as a hard signal to make a decision itself rather than retry.
var ErrAskUserNoChannel = errors.New("ask_user: no interactive channel available (no WebUI client connected and stdin is not a TTY)")

// AskUserManager coordinates ask_user requests between the agent
// and the webui. It follows the same pattern as security.ApprovalManager
// but returns string responses instead of bool.
type AskUserManager struct {
	mu      sync.Mutex
	pending map[string]chan string // requestID -> response channel
	// sensitive tracks which pending requests are credential requests and
	// which credential key their response belongs to. On respond, the value
	// is diverted to the credential store and the model receives only a
	// confirmation — the secret itself never reaches the model context.
	sensitive map[string]string // requestID -> credential store key
	timeout   time.Duration
}

const DefaultAskUserTimeout = 30 * time.Minute

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
func (m *AskUserManager) RequestAskUser(ctx context.Context, eventBus *events.EventBus, req AskUserRequest, clientID, userID, chatID string) (string, error) {
	if eventBus == nil {
		return "", fmt.Errorf("no event bus available")
	}

	if strings.TrimSpace(req.Question) == "" {
		return "", fmt.Errorf("empty question provided")
	}

	requestID := generateAskUserRequestID()
	responseCh := make(chan string, 1)

	m.mu.Lock()
	m.pending[requestID] = responseCh
	if req.Sensitive {
		if m.sensitive == nil {
			m.sensitive = make(map[string]string)
		}
		m.sensitive[requestID] = req.CredentialKey
	}
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		delete(m.pending, requestID)
		delete(m.sensitive, requestID)
		m.mu.Unlock()
	}()

	payload := events.AskUserRequestEvent(requestID, toEventRequest(req), clientID)
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
			eventBus.Publish(events.EventTypeAskUserRequest, events.AskUserCancelledEvent(requestID, clientID))
			return "", fmt.Errorf("response channel closed")
		}
		return result, nil
	case <-timer.C:
		log.Printf("Ask user request %s timed out after %v", requestID, timeout)
		eventBus.Publish(events.EventTypeAskUserRequest, events.AskUserCancelledEvent(requestID, clientID))
		return "", fmt.Errorf("user did not respond within %v", timeout)
	case <-ctx.Done():
		log.Printf("Ask user request %s cancelled: %v", requestID, ctx.Err())
		eventBus.Publish(events.EventTypeAskUserRequest, events.AskUserCancelledEvent(requestID, clientID))
		return "", fmt.Errorf("ask_user cancelled: %w", ctx.Err())
	}
}

// RespondToAskUser resolves a pending ask_user request with the user's text
// response. Returns true if the request existed and was responded to, false
// otherwise.
//
// For sensitive (credential) requests the response never reaches the model:
// it is written straight to the credential store under the request's key and
// the pending channel receives a masked confirmation instead. The result
// string the model sees is therefore safe to transcribe into the
// conversation.
func (m *AskUserManager) RespondToAskUser(requestID string, response string) bool {
	m.mu.Lock()
	ch, exists := m.pending[requestID]
	credKey, sensitive := m.sensitive[requestID]
	m.mu.Unlock()

	if !exists {
		return false
	}

	if sensitive {
		return m.respondSensitive(requestID, ch, credKey, response)
	}

	select {
	case ch <- response:
		return true
	default:
		return false
	}
}

// respondSensitive stores the user's value in the credential backend and
// feeds the pending channel a confirmation placeholder. A store failure is
// delivered to the model as an explicit error result (never the value) so
// the flow fails loudly and safely.
func (m *AskUserManager) respondSensitive(requestID string, ch chan string, credKey, response string) bool {
	if strings.TrimSpace(credKey) == "" {
		// Misconfigured request: fail closed with a message, not the value.
		select {
		case ch <- "ask_user: sensitive request had no credential key; nothing was stored. Ask the user to retry with the target key configured.":
			return true
		default:
			return false
		}
	}
	if strings.TrimSpace(response) == "" {
		select {
		case ch <- "ask_user: the user submitted an empty credential; nothing was stored. Ask again or choose another path.":
			return true
		default:
			return false
		}
	}
	if err := credentials.SetToActiveBackend(credKey, response); err != nil {
		log.Printf("[ask_user] failed to store credential %s for request %s: %v", credKey, requestID, err)
		select {
		case ch <- fmt.Sprintf("ask_user: storing the credential failed (%v). The value was NOT saved; ask the user to retry or use Settings.", err):
			return true
		default:
			return false
		}
	}
	// Log only metadata, never the value.
	log.Printf("[ask_user] credential stored for request %s (key %s, %d chars)", requestID, credKey, len(response))
	select {
	case ch <- fmt.Sprintf("Credential stored securely under %s. It is not visible in this conversation. Continue: restart or refresh the MCP server so it picks up the credential, then verify with mcp_refresh (operation: list).", credKey):
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
// Renders options as a numbered list when present and accepts either an
// index, the option label, or the option value as the response.
//
// On a TTY with single-select options (MultiSelect == false), the
// options are rendered as an arrow-key picker (console.SelectList)
// with a trailing "Type your own answer…" item that falls through to
// the legacy freeform input reader. Multi-select prompts and prompts
// on a non-TTY stdin fall back to the numbered list + freeform text
// path so the tool remains scriptable.
//
// The context governs cancellation: when ctx is cancelled (tool-execution
// timeout, interrupt, etc.) the function returns ctx.Err() immediately
// so the deferred terminal-state restoration hooks fire and the caller
// gets a clean error instead of silently swallowing the timeout.
//
// Returns ErrAskUserNoChannel if stdin is not a TTY (background daemon,
// closed stdin, piped) so callers can distinguish "no input channel"
// from a transient I/O error.
func AskUser(ctx context.Context, req AskUserRequest) (string, error) {
	if strings.TrimSpace(req.Question) == "" {
		return "", fmt.Errorf("empty question provided")
	}
	if !stdinIsTTY() {
		return "", ErrAskUserNoChannel
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
	clihooks.SuspendStreaming()
	defer clihooks.ResumeIndicator()
	defer clihooks.ResumeSteer()
	defer clihooks.ResumeStreaming()

	// TTY single-select path: use the arrow-key picker instead of the
	// numbered list. We only branch here when the input channel is a
	// real TTY AND we're not in multi-select mode (SelectList doesn't
	// support multi-select, and the legacy "1,3" comma-list text input
	// remains the canonical path for that case).
	if len(req.Options) > 0 && !req.MultiSelect {
		return runAskUserSelectList(ctx, req)
	}

	renderCLIPrompt(os.Stdout, req)

	// Sensitive (credential) prompts read a single line without echo —
	// the value must not land in the terminal scrollback or the
	// conversation. The caller (the ask_user handler) stores what we
	// return via RespondSensitiveCLI's contract: an empty answer means
	// "nothing stored".
	if req.Sensitive {
		fmt.Println("(input hidden — paste the value and press Enter)")
		secret, readErr := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println()
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return "", ErrAskUserNoChannel
			}
			return "", fmt.Errorf("read credential: %w", readErr)
		}
		return string(secret), nil
	}

	reader := bufio.NewReader(os.Stdin)

	if len(req.Options) == 0 {
		// Freeform mode: read until a blank line (double Enter) or EOF.
		// This allows pasting multiline text — a single newline between
		// pasted lines is preserved; the user submits by pressing Enter
		// on an empty line.
		answer, err := readFreeformInputCtx(ctx, reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return "", ErrAskUserNoChannel
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return "", err
			}
			return "", fmt.Errorf("read user input: %w", err)
		}
		if answer == "" && req.Default != "" {
			return req.Default, nil
		}
		return answer, nil
	}

	// Option mode: single line is sufficient.
	answer, err := readLineCtx(ctx, reader)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return "", ErrAskUserNoChannel
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", err
		}
		return "", fmt.Errorf("read user input: %w", err)
	}
	answer = strings.TrimSpace(answer)

	resolved, ok := resolveCLIOptionAnswer(answer, req)
	if !ok {
		return "", fmt.Errorf("invalid selection %q — expected a number 1-%d, an option label, or one of the option values", answer, len(req.Options))
	}
	return resolved, nil
}

func toEventRequest(req AskUserRequest) events.AskUserRequest {
	out := events.AskUserRequest{
		Question:      req.Question,
		Header:        req.Header,
		MultiSelect:   req.MultiSelect,
		Default:       req.Default,
		Sensitive:     req.Sensitive,
		CredentialKey: req.CredentialKey,
	}
	if len(req.Options) > 0 {
		out.Options = make([]events.AskUserRequestOption, len(req.Options))
		for i, opt := range req.Options {
			out.Options[i] = events.AskUserRequestOption{
				Label:       opt.Label,
				Value:       opt.Value,
				Description: opt.Description,
			}
		}
	}
	return out
}

func optionValue(opt AskUserOption) string {
	if strings.TrimSpace(opt.Value) != "" {
		return opt.Value
	}
	return opt.Label
}

// AskUserWithEventBus prompts the user with a question using the event bus
// for WebUI mode, falling back to stdin for CLI mode.
func AskUserWithEventBus(ctx context.Context, req AskUserRequest, eventBus *events.EventBus, clientID, userID, chatID string, mgr *AskUserManager) (string, error) {
	if strings.TrimSpace(req.Question) == "" {
		return "", fmt.Errorf("empty question provided")
	}

	// WebUI mode: route through event bus
	if mgr != nil && eventBus != nil {
		log.Printf("[ask_user] Routing through event bus: clientID=%q chatID=%q options=%d", clientID, chatID, len(req.Options))
		return mgr.RequestAskUser(ctx, eventBus, req, clientID, userID, chatID)
	}

	if mgr == nil {
		log.Printf("[ask_user] Global AskUserManager is nil — falling back to stdin (WebUI not initialized?)")
	}
	if eventBus == nil {
		log.Printf("[ask_user] Event bus is nil — falling back to stdin")
	}

	// CLI mode: read from stdin
	return AskUser(ctx, req)
}
