//go:build js

// WASM stubs for security classifier audit hooks.
//
// The real implementation lives in security_audit_hooks.go (//go:build !js),
// which depends on AuditLogger / AuditEntry from security_audit.go — types
// that are intentionally excluded from the js/wasm build because they rely on
// os/sync/file APIs unavailable in that environment.
//
// This file provides a no-op logSecurityDecision so that
// security_classifier.go (compiled for every target, including js) can call
// the symbol unconditionally. There is intentionally no SetAuditLogger stub
// here: audit logging is server-side plumbing that is never reachable under
// GOARCH=wasm.

package tools

// logSecurityDecision is a no-op under GOARCH=wasm, where AuditLogger is
// unavailable and auditing is not performed.
func logSecurityDecision(toolName string, result SecurityResult) {}
