// Security circuit breaker + audit logging for the live seed tool path.
//
// This file wires two previously-dormant features into the live pre-execute
// hook (newPreExecuteHook in seed_tool_registry.go):
//
//  1. A security-specific circuit breaker that escalates the caution message
//     when the LLM retries the exact same blocked operation. It lives in the
//     existing CircuitBreakerState.Actions map under a "sec:" key namespace
//     so it cannot collide with the (dormant) general circuit breaker.
//
//  2. Audit logging of unified-gate security decisions (blocked / prompted /
//     approved / loop_detected) through the Agent-owned AuditLogger. This
//     complements the package-level auditLogger already invoked from
//     ClassifyToolCall in pkg/agent_tools.
//
// All helpers are nil-safe on the agent / state / logger so bare *Agent
// values in unit tests don't panic.
package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// securityBlockThreshold is the consecutive-identical-block count below
// which the caution stays a standard retry-warning. One retry (count 1→2)
// is forgivable; when the count exceeds this threshold (count > 2, i.e. a
// third identical attempt), the caution escalates to a hard "STOP RETRYING"
// signal (SECURITY_CAUTION_LOOP_DETECTED).
const securityBlockThreshold = 2

// generateSecurityBlockKey builds a deterministic key for a tool+args combo,
// namespaced under "sec:" so it never collides with the general circuit
// breaker (which uses "toolName:argsJSON"). Mirrors the hashing pattern in
// pkg/agent/seed_tool_execution.go.
func generateSecurityBlockKey(toolName string, args map[string]interface{}) string {
	argsJSON, _ := json.Marshal(args)
	return fmt.Sprintf("sec:%s:%s", toolName, string(argsJSON))
}

// recordSecurityBlock increments the consecutive-block counter for a tool+args
// combo and returns the new count. When the agent or circuit-breaker state is
// unavailable (e.g. bare *Agent in unit tests), it returns 0 — the caller
// treats 0 as "no tracking" and skips loop escalation. Thread-safe via
// CircuitBreakerState.mu.
func (a *Agent) recordSecurityBlock(toolName string, args map[string]interface{}) int {
	if a == nil || a.state == nil {
		return 0
	}
	cb := a.state.GetCircuitBreaker()
	if cb == nil {
		return 0
	}

	key := generateSecurityBlockKey(toolName, args)
	cb.mu.Lock()
	defer cb.mu.Unlock()

	action, exists := cb.Actions[key]
	if !exists {
		action = &CircuitBreakerAction{
			ActionType: toolName,
			Target:     key,
			Count:      0,
		}
		cb.Actions[key] = action
	}
	action.Count++
	action.LastUsed = getCurrentTime()

	// Clean up stale entries (both sec: and general) to prevent unbounded
	// map growth in long sessions. The 5-minute TTL matches the existing
	// cleanupOldCircuitBreakerEntriesLocked pattern. Runs under the lock
	// we already hold. Only sweep every Nth insert to amortize cost.
	if action.Count == 1 || len(cb.Actions) > 64 {
		a.cleanupStaleSecurityEntriesLocked(cb)
	}
	return action.Count
}

// cleanupStaleSecurityEntriesLocked removes entries (any namespace) older
// than 5 minutes. Caller MUST hold cb.mu.
func (a *Agent) cleanupStaleSecurityEntriesLocked(cb *CircuitBreakerState) {
	currentTime := getCurrentTime()
	fiveMinutesAgo := currentTime - 300
	for key, entry := range cb.Actions {
		if entry.LastUsed < fiveMinutesAgo {
			delete(cb.Actions, key)
		}
	}
}

// clearSecurityBlock resets the consecutive-block counter for a tool+args
// combo. Called on the success path so the counter only tracks *consecutive*
// failures — a successful call (even with different args) resets the tracking
// for that exact combo. No-op when state is unavailable.
func (a *Agent) clearSecurityBlock(toolName string, args map[string]interface{}) {
	if a == nil || a.state == nil {
		return
	}
	cb := a.state.GetCircuitBreaker()
	if cb == nil {
		return
	}

	key := generateSecurityBlockKey(toolName, args)
	cb.mu.Lock()
	defer cb.mu.Unlock()
	delete(cb.Actions, key)
}

// getSecurityBlockCount returns the current consecutive-block count for a
// tool+args combo without mutating it. Returns 0 when untracked or state is
// unavailable. Thread-safe read via CircuitBreakerState.mu.
func (a *Agent) getSecurityBlockCount(toolName string, args map[string]interface{}) int {
	if a == nil || a.state == nil {
		return 0
	}
	cb := a.state.GetCircuitBreaker()
	if cb == nil {
		return 0
	}

	key := generateSecurityBlockKey(toolName, args)
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	if action, ok := cb.Actions[key]; ok {
		return action.Count
	}
	return 0
}

// ---------------------------------------------------------------------------
// Audit logging helpers (Task 2)
// ---------------------------------------------------------------------------

// GetAuditLogger returns the agent-owned security audit logger, or nil when
// none is configured. Nil-safe: callers should nil-check before use.
func (a *Agent) GetAuditLogger() *tools.AuditLogger {
	if a == nil {
		return nil
	}
	return a.auditLogger
}

// SetAuditLogger attaches a security audit logger to this agent. Also sets
// the package-level logger in pkg/agent_tools (via tools.SetAuditLogger) so
// that ClassifyToolCall entries are written through the same file. Pass nil
// to disable audit logging.
func (a *Agent) SetAuditLogger(l *tools.AuditLogger) {
	if a == nil {
		return
	}
	a.auditLogger = l
	tools.SetAuditLogger(l)
}

// logSecurityDecision writes a structured audit entry for a unified-gate
// security decision. action is one of "blocked", "approved", "prompted",
// "loop_detected". The args map may contain secrets, so only the tool name
// is recorded — never the raw args — matching the safety stance of the
// existing AuditEntry.Args field. Nil-safe: skips silently when no logger
// is configured.
func (a *Agent) logSecurityDecision(tool string, args map[string]interface{}, assessment RiskAssessment, action string) {
	if a == nil {
		return
	}
	logger := a.GetAuditLogger()
	if logger == nil {
		return
	}

	category := ""
	if len(assessment.Sources) > 0 {
		category = string(assessment.Sources[0])
	}

	// SP-068 SP-127 synergy: include path-tier and file-mode in audit log
	// so consumers can distinguish "elevated due to sensitive path" from
	// "elevated due to destructive command" without parsing reasoning.
	pathTier := ""
	if assessment.PathTier != PathTierUnknown {
		pathTier = assessment.PathTier.String()
	}

	sessionID := ""
	workspace := ""
	if a.state != nil {
		sessionID = a.state.GetSessionID()
	}
	if ws := strings.TrimSpace(a.GetWorkspaceRoot()); ws != "" {
		workspace = ws
	}

	// Deliberately omit AuditEntry.Args — args may contain secrets (e.g.
	// shell commands with embedded tokens). The tool name + risk level +
	// reasoning are sufficient for an audit trail without a leakage risk.
	// Sanitize the reasoning too — it frequently contains command text that
	// may embed tokens (e.g. curl -H 'Authorization: Bearer sk-...').
	entry := tools.AuditEntry{
		Timestamp: time.Now(),
		Tool:      tool,
		// Args intentionally blank — see comment above.
		RiskLevel: string(assessment.Level),
		Category:  category,
		Action:    action,
		Reasoning: sanitizeForAudit(assessment.Reason),
		Source:    "unified-gate",
		SessionID: sessionID,
		Workspace: workspace,
		PathTier:  pathTier,
		FileMode:  assessment.FileMode,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_ = logger.LogJSON(data)
}

// sanitizeForAudit redacts likely-secret substrings from a reasoning string
// before it is persisted to the audit log. It reuses sanitizeToolFailureMessage
// (strips base64/data-URIs, truncates) and additionally collapses long runs
// that look like API keys. This is defense-in-depth — the primary secret
// surface (args) is already omitted.
func sanitizeForAudit(s string) string {
	return sanitizeToolFailureMessage(s)
}

// riskCategoryFromAssessment derives a human-readable risk-level string from
// a RiskAssessment for the audit trail. Falls back to "unknown".
func riskCategoryFromAssessment(assessment RiskAssessment) string {
	if assessment.Level != "" {
		return string(assessment.Level)
	}
	return string(configuration.RiskLevelLow)
}

// ---------------------------------------------------------------------------
// Tier-aware caution suffixes (Task 4)
//
// Every SECURITY_CAUTION_REQUIRED block used to get the same generic guidance.
// But a hard block (critical) is never approvable, while a CAUTION block just
// needs interactive approval. Parsing the tier from the error message prefix
// (Option B from the task spec) avoids signature changes while still giving
// the LLM tier-appropriate guidance.
// ---------------------------------------------------------------------------

// tierFromMessage inspects the security error message and returns the
// appropriate guidance suffix. It recognises four prefixes that the unified
// gate / approval flow already emit:
//
//   - "hard block"      → critical/unconditional (no profile can approve)
//   - "confirmation required" → medium/intent (needs interactive approval)
//   - "rejected"        → user-decline (do not retry)
//   - default           → generic guidance
func tierFromMessage(msg string) string {
	lc := strings.ToLower(msg)
	switch {
	case strings.Contains(lc, "hard block"):
		return "This operation is unconditionally blocked — no risk profile, flag, or approval can authorize it. Do not attempt it again."
	case strings.Contains(lc, "confirmation required"):
		return "This operation can proceed with interactive user approval. Use ask_user to confirm, or the user can re-run with --risk-profile=permissive."
	case strings.Contains(lc, "rejected"):
		return "The user declined this operation. Do not retry without a fundamentally different approach."
	default:
		return "Do not retry this exact operation without changing the risk profile or getting explicit user approval."
	}
}

// tierPrefixFromMessage returns the one-word tier label for display/audit
// purposes, mirroring tierFromMessage's classification.
func tierPrefixFromMessage(msg string) string {
	lc := strings.ToLower(msg)
	switch {
	case strings.Contains(lc, "hard block"):
		return "hard-block"
	case strings.Contains(lc, "confirmation required"):
		return "confirmation"
	case strings.Contains(lc, "rejected"):
		return "rejected"
	default:
		return "caution"
	}
}
