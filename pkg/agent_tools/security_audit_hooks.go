//go:build !js

// Audit logging hooks for security classifier decisions.
//
// This file is compiled for all non-WASM targets. It wires the string-based
// security classifier (security_classifier.go, which has no build tag and is
// compiled everywhere) to the AuditLogger / AuditEntry types defined in
// security_audit.go (which carry their own //go:build !js tag because they
// depend on os/sync/file APIs unavailable under GOARCH=wasm).
//
// The companion file security_audit_hooks_wasm.go provides a no-op
// logSecurityDecision for the js/wasm build, so the classifier can call it
// unconditionally regardless of target.

package tools

import (
	"log"
	"sync/atomic"
	"time"
)

// auditLogger is the package-level audit logger for security decisions.
// Set via SetAuditLogger; accessed atomically for concurrent safety.
var auditLogger atomic.Pointer[AuditLogger]

// SetAuditLogger sets the package-level audit logger for recording security
// decisions. Must be called during initialization before concurrent goroutines
// begin calling ClassifyToolCall.
//
// Only compiled for non-js targets — the audit logger is server-side
// plumbing that is never available under GOARCH=wasm.
func SetAuditLogger(l *AuditLogger) {
	auditLogger.Store(l)
}

// logSecurityDecision records a security classification decision to the audit
// log, if a logger has been configured via SetAuditLogger. It is nil-safe
// (a no-op when no logger is set) and non-fatal (logging errors are reported
// via the standard logger but never propagate).
//
// Under GOARCH=wasm this symbol is replaced by an empty no-op stub (see
// security_audit_hooks_wasm.go) because AuditLogger/AuditEntry are not
// available there.
func logSecurityDecision(toolName string, result SecurityResult) {
	if l := auditLogger.Load(); l != nil {
		if err := l.LogEntry(AuditEntry{
			Timestamp: time.Now(),
			Tool:      toolName,
			RiskLevel: result.Risk.String(),
			Category:  string(result.Category),
			Action:    classifyAction(result),
			Reasoning: result.Reasoning,
			Source:    "classifier",
		}); err != nil {
			log.Printf("audit log write failed: %v", err)
		}
	}
}
