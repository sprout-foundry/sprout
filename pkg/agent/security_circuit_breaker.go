// Security circuit breaker + audit logging for the live seed tool path.
package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// securityBlockThreshold is the consecutive-identical-block count before escalation.
const securityBlockThreshold = 2

// generateSecurityBlockKey builds a deterministic key for a tool+args combo, namespaced under "sec:".
func generateSecurityBlockKey(toolName string, args map[string]interface{}) string {
	argsJSON, _ := json.Marshal(args)
	return fmt.Sprintf("sec:%s:%s", toolName, string(argsJSON))
}

// recordSecurityBlock increments the consecutive-block counter and returns the new count.
// Returns 0 when state is unavailable. Thread-safe via CircuitBreakerState.mu.
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

	// Clean up stale entries every Nth insert to amortize cost.
	if action.Count == 1 || len(cb.Actions) > 64 {
		a.cleanupStaleSecurityEntriesLocked(cb)
	}
	return action.Count
}

// cleanupStaleSecurityEntriesLocked removes entries older than 5 minutes. Caller MUST hold cb.mu.
func (a *Agent) cleanupStaleSecurityEntriesLocked(cb *CircuitBreakerState) {
	currentTime := getCurrentTime()
	fiveMinutesAgo := currentTime - 300
	for key, entry := range cb.Actions {
		if entry.LastUsed < fiveMinutesAgo {
			delete(cb.Actions, key)
		}
	}
}

// clearSecurityBlock resets the consecutive-block counter for a tool+args combo.
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

// getSecurityBlockCount returns the current consecutive-block count without mutating it.
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

// GetAuditLogger returns the agent-owned security audit logger, or nil.
func (a *Agent) GetAuditLogger() *tools.AuditLogger {
	if a == nil {
		return nil
	}
	return a.auditLogger
}

// SetAuditLogger attaches a security audit logger to this agent. Also sets
// the package-level logger in pkg/agent_tools. Pass nil to disable.
func (a *Agent) SetAuditLogger(l *tools.AuditLogger) {
	if a == nil {
		return
	}
	a.auditLogger = l
	tools.SetAuditLogger(l)
}

// logSecurityDecision writes a structured audit entry for a unified-gate security decision.
// Args are deliberately omitted to avoid secret leakage. Nil-safe: skips when no logger.
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

	entry := tools.AuditEntry{
		Timestamp: time.Now(),
		Tool:      tool,
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

// sanitizeForAudit redacts likely-secret substrings before persisting to the audit log.
func sanitizeForAudit(s string) string {
	return sanitizeToolFailureMessage(s)
}

// riskCategoryFromAssessment derives a human-readable risk-level string for the audit trail.
func riskCategoryFromAssessment(assessment RiskAssessment) string {
	if assessment.Level != "" {
		return string(assessment.Level)
	}
	return string(configuration.RiskLevelLow)
}

// tierFromMessage inspects the security error message and returns the appropriate guidance suffix.
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
