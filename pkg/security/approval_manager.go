package security

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// ApprovalDecision is the user's choice from the 4-option approval dialog
// (SP-058 follow-up). The classic yes/no path collapses to ApprovalDeny or
// ApprovalApproveOnce so legacy bool wrappers still work; the two extra
// outcomes — ApprovalApproveAlways and ApprovalElevate — power the
// "always approve this command" and "elevate session permissions" buttons
// added to the WebUI dialog and the CLI prompt.
type ApprovalDecision int

const (
	// ApprovalDeny rejects the operation. Caller surfaces a security error.
	ApprovalDeny ApprovalDecision = iota
	// ApprovalApproveOnce approves this single invocation. Subsequent
	// invocations of the same command still prompt.
	ApprovalApproveOnce
	// ApprovalApproveAlways approves this invocation AND persists the
	// exact command string to Config.ApprovedShellCommands so future
	// runs (across restarts) skip the prompt for the same literal command.
	ApprovalApproveAlways
	// ApprovalElevate approves this invocation AND sets the agent's
	// risk profile override to "permissive" for the rest of this
	// session. Critical-tier ops still block. Persistence is session
	// only — the CLI/WebUI should tell the user to use /risk-profile
	// permissive if they want this to survive restart.
	ApprovalElevate

	// ApprovalAllowFolderSession is the filesystem-tier-B outcome:
	// approve this invocation AND add the prompt's target folder to
	// the agent's session-allowed folder list (in-memory). Subsequent
	// accesses under that folder skip the prompt for the rest of the
	// session. The folder is conveyed via the approval request's
	// `folder` extra so the caller knows which path to record.
	ApprovalAllowFolderSession

	// ApprovalAlwaysAsk approves this invocation AND persists the
	// command as an "ask" rule in Config.CommandPolicies so future
	// matching commands always force an interactive prompt, even in
	// permissive mode or when the classifier says SAFE. SP-123-2b.
	ApprovalAlwaysAsk
)

// String returns a stable lowercase identifier for the decision, used
// in event payloads and tests.
func (d ApprovalDecision) String() string {
	switch d {
	case ApprovalDeny:
		return "deny"
	case ApprovalApproveOnce:
		return "approve_once"
	case ApprovalApproveAlways:
		return "approve_always"
	case ApprovalElevate:
		return "elevate"
	case ApprovalAllowFolderSession:
		return "allow_folder_session"
	case ApprovalAlwaysAsk:
		return "always_ask"
	default:
		return "deny"
	}
}

// ApprovalDecisionFromString parses a wire-format decision string back
// into the typed enum. Unknown values resolve to ApprovalDeny for safety.
func ApprovalDecisionFromString(s string) ApprovalDecision {
	switch s {
	case "approve_once", "approve", "yes", "true":
		return ApprovalApproveOnce
	case "approve_always", "always":
		return ApprovalApproveAlways
	case "elevate":
		return ApprovalElevate
	case "allow_folder_session":
		return ApprovalAllowFolderSession
	case "always_ask", "ask":
		return ApprovalAlwaysAsk
	default:
		return ApprovalDeny
	}
}

// Approved reports whether the operation should proceed (any non-Deny
// decision approves the current invocation).
func (d ApprovalDecision) Approved() bool {
	return d != ApprovalDeny
}

// ApprovalKind distinguishes between the two approval flows that share
// this manager. The kind determines the event type published, the request
// ID prefix, and the default response used on timeout / nil event-bus.
type ApprovalKind int

const (
	// ApprovalKindTool is used for tool execution approval (shell_command,
	// write_file, git ops, etc.). Timeout or nil event-bus rejects for safety.
	ApprovalKindTool ApprovalKind = iota

	// ApprovalKindPrompt is used for file content security prompts (API keys,
	// passwords found in files). Timeout or nil event-bus returns DefaultResponse.
	ApprovalKindPrompt
)

// ApprovalRequest captures all parameters for a single approval request.
type ApprovalRequest struct {
	Kind ApprovalKind

	// On timeout / nil event-bus: PromptKind returns DefaultResponse;
	// ToolKind always returns false (reject for safety).
	DefaultResponse bool

	// Tool kind fields
	ToolName  string
	RiskLevel string
	Reasoning string
	ClientID  string
	UserID    string // User ID for multi-tenant isolation

	// Prompt kind fields
	Prompt string

	// Shared extras forwarded to the event payload.
	Extras map[string]string
}

// ApprovalOutcome reports HOW an approval request resolved, independent of
// the decision itself. It lets callers distinguish a deliberate user "deny"
// from a dialog that was never answered — so an unattended browser tab can
// fall back to the terminal prompt instead of dead-ending on a 5-minute
// timeout that silently denies.
type ApprovalOutcome int

const (
	// ApprovalOutcomeResponded — the user (browser) actually answered.
	// The accompanying decision is authoritative; honor it.
	ApprovalOutcomeResponded ApprovalOutcome = iota
	// ApprovalOutcomeTimedOut — no answer arrived within the timeout
	// window. The decision is the safe default; callers may retry on
	// another surface.
	ApprovalOutcomeTimedOut
	// ApprovalOutcomeNoChannel — the event bus was nil or the response
	// channel closed without a reply (e.g. the browser disconnected).
	// The decision is the safe default; callers may retry on another surface.
	ApprovalOutcomeNoChannel
)

// ApprovalManager coordinates security approval requests between the agent
// and the webui. It subsumes the former SecurityApprovalManager (tool
// approvals) and SecurityPromptManager (file security prompts) into a single
// unified manager, eliminating duplicated infrastructure.
//
// The manager is safe for concurrent use. Requests block until a response is
// received from the webui, a timeout elapses, or the event bus is nil.
type ApprovalManager struct {
	mu      sync.Mutex
	pending map[string]chan ApprovalDecision // requestID -> response channel
	timeout time.Duration                    // per-request timeout; 0 ⇒ DefaultTimeout
}

// DefaultTimeout is the maximum time a request will block waiting for a
// webui response before applying the fallback (reject for tool kind,
// defaultResponse for prompt kind).
//
// Set generously (30 min) so a user who steps away from an interactive
// session can still return and approve — a false-deny after 5 minutes was
// a recurring UX complaint. Non-interactive runs should not hit this path
// at all (they fast-fail before calling the manager), so the long timeout
// has no downside for automation.
const DefaultTimeout = 30 * time.Minute

// --- Global singleton ---

var (
	globalApprovalManager   *ApprovalManager
	globalApprovalManagerMu sync.RWMutex
)

// SetGlobalApprovalManager sets the global singleton (called by webui setup).
//
// Deprecated: use dependency injection via Agent.InjectWebUIManagers instead.
func SetGlobalApprovalManager(mgr *ApprovalManager) {
	globalApprovalManagerMu.Lock()
	globalApprovalManager = mgr
	globalApprovalManagerMu.Unlock()
}

// GetGlobalApprovalManager returns the global singleton.
//
// Deprecated: use dependency injection via Agent.InjectWebUIManagers instead.
func GetGlobalApprovalManager() *ApprovalManager {
	globalApprovalManagerMu.RLock()
	defer globalApprovalManagerMu.RUnlock()
	return globalApprovalManager
}

// Backward-compatible aliases so existing code referencing the old names
// continues to compile during the migration window.
var (
	// SetGlobalPromptManager is a backward-compatible alias for SetGlobalApprovalManager.
	SetGlobalPromptManager = SetGlobalApprovalManager
	// GetGlobalPromptManager is a backward-compatible alias for GetGlobalApprovalManager.
	GetGlobalPromptManager = GetGlobalApprovalManager
)

// NewApprovalManager creates a new ApprovalManager with the default timeout.
func NewApprovalManager() *ApprovalManager {
	return &ApprovalManager{
		pending: make(map[string]chan ApprovalDecision),
		timeout: DefaultTimeout,
	}
}

// --- Request ID generation ---

var (
	nextReqID   int64
	nextReqIDMu sync.Mutex
)

func generateToolRequestID() string {
	nextReqIDMu.Lock()
	defer nextReqIDMu.Unlock()
	nextReqID++
	return fmt.Sprintf("sec_%d", nextReqID)
}

func generatePromptRequestID() string {
	nextReqIDMu.Lock()
	defer nextReqIDMu.Unlock()
	nextReqID++
	return fmt.Sprintf("sec_prompt_%d", nextReqID)
}

// --- Core API ---

// RequestApproval publishes a security approval/prompt event to the event
// bus and blocks until the webui responds, a timeout elapses, or the event
// bus is nil.
//
// For ToolKind: returns true only if explicitly approved; false on rejection,
// timeout, or nil event-bus.
// For PromptKind: returns the user response, or DefaultResponse on timeout /
// nil event-bus.
//
// Callers that need the richer 4-option outcome (ApproveAlways / Elevate)
// should call RequestApprovalDecision; this wrapper collapses to bool.
func (am *ApprovalManager) RequestApproval(eventBus *events.EventBus, req ApprovalRequest) bool {
	return am.RequestApprovalDecision(eventBus, req).Approved()
}

// RequestApprovalDecision is the same as RequestApproval but returns the
// full ApprovalDecision so the caller can distinguish ApproveOnce from
// ApproveAlways and Elevate. The 4-option UI is currently only wired for
// shell_command Gate 1/2 callers; everyone else collapses through the
// bool wrapper above.
func (am *ApprovalManager) RequestApprovalDecision(eventBus *events.EventBus, req ApprovalRequest) ApprovalDecision {
	decision, _ := am.RequestApprovalDecisionWithOutcome(eventBus, req)
	return decision
}

// RequestApprovalDecisionWithOutcome is the same as RequestApprovalDecision
// but also returns an ApprovalOutcome so the caller can tell whether the
// user actually answered (Responded) or the request fell back to its safe
// default via timeout / missing channel. Callers that have an alternate
// approval surface (e.g. a terminal prompt) use this to avoid treating an
// unanswered browser dialog as a deliberate deny.
func (am *ApprovalManager) RequestApprovalDecisionWithOutcome(eventBus *events.EventBus, req ApprovalRequest) (ApprovalDecision, ApprovalOutcome) {
	if eventBus == nil {
		return am.defaultDecisionForKind(req), ApprovalOutcomeNoChannel
	}

	requestID := am.generateRequestID(req.Kind)
	responseCh := make(chan ApprovalDecision, 1)

	am.mu.Lock()
	am.pending[requestID] = responseCh
	am.mu.Unlock()

	defer func() {
		am.mu.Lock()
		delete(am.pending, requestID)
		am.mu.Unlock()
	}()

	// Build and publish the appropriate event type.
	switch req.Kind {
	case ApprovalKindTool:
		payload := events.SecurityApprovalRequestEvent(
			requestID, req.ToolName, req.RiskLevel, req.Reasoning, req.Extras,
		)
		if trimmed := strings.TrimSpace(req.ClientID); trimmed != "" {
			payload["client_id"] = trimmed
		}
		if trimmed := strings.TrimSpace(req.UserID); trimmed != "" {
			payload["user_id"] = trimmed
		}
		eventBus.Publish(events.EventTypeSecurityApprovalRequest, payload)
		eventBus.Publish(events.EventTypeInputRequired, events.InputRequiredEvent("security_approval", requestID))

	case ApprovalKindPrompt:
		payload := events.SecurityPromptRequestEvent(requestID, req.Prompt, req.DefaultResponse, req.Extras)
		if trimmed := strings.TrimSpace(req.UserID); trimmed != "" {
			payload["user_id"] = trimmed
		}
		eventBus.Publish(events.EventTypeSecurityPromptRequest, payload)
		eventBus.Publish(events.EventTypeInputRequired, events.InputRequiredEvent("security_prompt", requestID))
	}

	timeout := am.timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	// Announce the wait so a silent multi-minute pause is diagnosable from
	// the log instead of looking like the agent is wedged.
	log.Printf("[approval] request %s (%v/%s) waiting up to %v for user response", requestID, req.Kind, req.ToolName, timeout)

	select {
	case result, ok := <-responseCh:
		if !ok {
			return am.defaultDecisionForKind(req), ApprovalOutcomeNoChannel // channel closed without response
		}
		return result, ApprovalOutcomeResponded
	case <-timer.C:
		log.Printf("Security approval request %s timed out after %v — applying default", requestID, timeout)
		return am.defaultDecisionForKind(req), ApprovalOutcomeTimedOut
	}
}

// RespondToApproval resolves a pending request with a boolean (legacy
// path: collapses to ApprovalApproveOnce or ApprovalDeny). Returns true
// if the request existed and was responded to.
func (am *ApprovalManager) RespondToApproval(requestID string, response bool) bool {
	d := ApprovalDeny
	if response {
		d = ApprovalApproveOnce
	}
	return am.RespondToApprovalDecision(requestID, d)
}

// RespondToApprovalDecision resolves a pending request with the full
// 4-option decision. The WebUI handler uses this to forward the user's
// "always approve" or "elevate" choice; the bool wrapper RespondToApproval
// stays for any caller (CLI bridge, tests) that only knows yes/no.
func (am *ApprovalManager) RespondToApprovalDecision(requestID string, decision ApprovalDecision) bool {
	am.mu.Lock()
	ch, exists := am.pending[requestID]
	am.mu.Unlock()

	if !exists {
		return false
	}

	select {
	case ch <- decision:
		return true
	default:
		return false
	}
}

// RespondToPrompt is a backward-compatible alias for RespondToApproval.
func (am *ApprovalManager) RespondToPrompt(requestID string, response bool) bool {
	return am.RespondToApproval(requestID, response)
}

// SetTimeout sets the maximum duration requests will block. A zero or
// negative value resets to the default.
func (am *ApprovalManager) SetTimeout(d time.Duration) {
	am.mu.Lock()
	defer am.mu.Unlock()
	if d <= 0 {
		am.timeout = DefaultTimeout
	} else {
		am.timeout = d
	}
}

// --- Convenience wrappers preserving the old call signatures ---

// RequestToolApproval is a convenience wrapper for ApprovalKindTool requests.
// It preserves the original SecurityApprovalManager.RequestApproval signature.
func (am *ApprovalManager) RequestToolApproval(eventBus *events.EventBus, clientID, userID, toolName, riskLevel, reasoning string, extras map[string]string) bool {
	return am.RequestToolApprovalDecision(eventBus, clientID, userID, toolName, riskLevel, reasoning, extras).Approved()
}

// RequestToolApprovalDecision is the 4-option variant: it returns the
// full ApprovalDecision so callers can react to "always approve" and
// "elevate" choices. Wire payload is the same SecurityApprovalRequest;
// the WebUI dialog opts into the extra buttons via the matching response
// schema (action field).
func (am *ApprovalManager) RequestToolApprovalDecision(eventBus *events.EventBus, clientID, userID, toolName, riskLevel, reasoning string, extras map[string]string) ApprovalDecision {
	return am.RequestApprovalDecision(eventBus, ApprovalRequest{
		Kind:            ApprovalKindTool,
		DefaultResponse: false, // reject for safety
		ToolName:        toolName,
		RiskLevel:       riskLevel,
		Reasoning:       reasoning,
		ClientID:        clientID,
		UserID:          userID,
		Extras:          extras,
	})
}

// RequestToolApprovalWithOutcome is the outcome-aware variant of
// RequestToolApproval: it returns whether the operation was approved AND how
// the request resolved (Responded / TimedOut / NoChannel), so a caller with a
// terminal fallback can avoid treating an unanswered browser dialog as a deny.
func (am *ApprovalManager) RequestToolApprovalWithOutcome(eventBus *events.EventBus, clientID, userID, toolName, riskLevel, reasoning string, extras map[string]string) (bool, ApprovalOutcome) {
	decision, outcome := am.RequestToolApprovalDecisionWithOutcome(eventBus, clientID, userID, toolName, riskLevel, reasoning, extras)
	return decision.Approved(), outcome
}

// RequestToolApprovalDecisionWithOutcome is the outcome-aware variant of
// RequestToolApprovalDecision (the 4-option Gate 2 path).
func (am *ApprovalManager) RequestToolApprovalDecisionWithOutcome(eventBus *events.EventBus, clientID, userID, toolName, riskLevel, reasoning string, extras map[string]string) (ApprovalDecision, ApprovalOutcome) {
	return am.RequestApprovalDecisionWithOutcome(eventBus, ApprovalRequest{
		Kind:            ApprovalKindTool,
		DefaultResponse: false, // reject for safety
		ToolName:        toolName,
		RiskLevel:       riskLevel,
		Reasoning:       reasoning,
		ClientID:        clientID,
		UserID:          userID,
		Extras:          extras,
	})
}

// RequestPrompt is a convenience wrapper for ApprovalKindPrompt requests.
// It preserves the original SecurityPromptManager.RequestPrompt signature.
func (am *ApprovalManager) RequestPrompt(eventBus *events.EventBus, userID, prompt string, defaultResponse bool, extras map[string]string) bool {
	return am.RequestApproval(eventBus, ApprovalRequest{
		Kind:            ApprovalKindPrompt,
		DefaultResponse: defaultResponse,
		Prompt:          prompt,
		UserID:          userID,
		Extras:          extras,
	})
}

// SetApprovalTimeout is a backward-compatible alias for SetTimeout.
func (am *ApprovalManager) SetApprovalTimeout(d time.Duration) {
	am.SetTimeout(d)
}

// SetPromptTimeout is a backward-compatible alias for SetTimeout.
func (am *ApprovalManager) SetPromptTimeout(d time.Duration) {
	am.SetTimeout(d)
}

// --- Internal helpers ---

func (am *ApprovalManager) defaultForKind(req ApprovalRequest) bool {
	return am.defaultDecisionForKind(req).Approved()
}

func (am *ApprovalManager) defaultDecisionForKind(req ApprovalRequest) ApprovalDecision {
	switch req.Kind {
	case ApprovalKindTool:
		return ApprovalDeny // reject for safety
	case ApprovalKindPrompt:
		if req.DefaultResponse {
			return ApprovalApproveOnce
		}
		return ApprovalDeny
	default:
		return ApprovalDeny
	}
}

func (am *ApprovalManager) generateRequestID(kind ApprovalKind) string {
	switch kind {
	case ApprovalKindTool:
		return generateToolRequestID()
	case ApprovalKindPrompt:
		return generatePromptRequestID()
	default:
		return generateToolRequestID()
	}
}
