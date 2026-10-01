// Package agent: forwarders for the path-tier / access-mode foundation,
// moved to the approvals subpackage (SP-141 phase 3).
//
// The classifier and its helpers live in pkg/agent/approvals so they are
// reusable without a *Agent dependency. The aliases below keep every
// existing call site (risk_assessment.go, tool_security_paths.go,
// security_circuit_breaker.go, agent_security.go, computer_use_registration.go,
// submanager_security.go) working unchanged.
//
// detectHomeDir is a test-override hook: it delegates to the approvals
// implementation so that overriding it in a test keeps working for the
// agent-side callers (risk_assessment.go, tool_security_paths.go).
package agent

import (
	"github.com/sprout-foundry/sprout/pkg/agent/approvals"
)

// PathTier is the approval-tier classification of a filesystem path.
type PathTier = approvals.PathTier

const (
	PathTierUnknown   = approvals.PathTierUnknown
	PathTierWorkspace = approvals.PathTierWorkspace
	PathTierExternal  = approvals.PathTierExternal
	PathTierSensitive = approvals.PathTierSensitive
)

// ClassifyPathAccess decides which approval tier a path falls into.
func ClassifyPathAccess(path, workspaceRoot, homeDir, cwd string) PathTier {
	return approvals.ClassifyPathAccess(path, workspaceRoot, homeDir, cwd)
}

// normalizePath cleans a path for prefix comparison (case-insensitive on
// Windows, symlinks resolved).
func normalizePath(p string) string {
	return approvals.NormalizePath(p)
}

// canonicalPath returns the case-preserving clean form (the form to show
// a user); normalizePath is its case-folded Windows comparison form.
func canonicalPath(p string) string {
	return approvals.CanonicalPath(p)
}

// isUnderPrefix reports whether path equals or sits under prefix (component-aware).
func isUnderPrefix(path, prefix string) bool {
	return approvals.IsUnderPrefix(path, prefix)
}

// accessModeForTool returns "write" for mutating tools and "read" otherwise.
func accessModeForTool(toolName string) string {
	return approvals.AccessModeForTool(toolName)
}

// detectHomeDir returns the user's home directory, or "" if unresolved.
var detectHomeDir = approvals.DetectHomeDir
