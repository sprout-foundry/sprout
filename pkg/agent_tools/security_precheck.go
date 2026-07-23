package tools

import (
	"context"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// PrecheckFileAccess resolves a file path and consults Gate 1's
// path-tier classifier before a file operation runs. This is the M2
// entry point for file-touching handlers.
//
// SP-127 M3.2: ctx carries the audit logger; PrecheckFileAccess passes
// it to the classifier so every decision (allow/prompt/deny) is logged.
//
// Returns:
//   - resolvedPath: the symlink-evaluated canonical form (may equal filePath)
//   - decision: "allow", "prompt", or "deny" from the classifier
//
// Behavioral contract:
//   - "allow"  → caller proceeds directly with resolvedPath; no prompt fires
//   - "prompt" → caller falls through; returns raw filesystem error
//   - "deny"  → caller returns a typed error immediately; no prompt fires
//
// When classifier is nil (no agent context), returns ("", "prompt")
// so callers fall through and return the raw filesystem error.
//
// SP-127 M2: this function lives in pkg/agent_tools rather than
// pkg/agent so handlers can call it without creating an import cycle.
func PrecheckFileAccess(ctx context.Context, classifier FileAccessClassifier, toolName, filePath string) (resolvedPath string, decision string) {
	if classifier == nil {
		// No classifier available — fall through and return the raw filesystem error.
		return "", "prompt"
	}

	mode := accessModeForTool(toolName)
	resolved, resolveErr := filesystem.SafeResolvePath(filePath)

	if resolveErr != nil {
		// Path is outside workspace — classify it to determine whether
		// to prompt or deny. Use filePath as resolvedPath since the
		// canonical target couldn't be determined.
		verdict := classifier.ClassifyFileAccess(ctx, filePath, filePath, mode)
		return filePath, verdict
	}

	// Path resolved successfully — classify it to determine allow/prompt/deny.
	verdict := classifier.ClassifyFileAccess(ctx, filePath, resolved, mode)
	return resolved, verdict
}

// accessModeForTool returns "write" for mutation tools and "read" for read tools.
func accessModeForTool(toolName string) string {
	switch toolName {
	case "write_file", "edit_file", "write_structured_file", "patch_structured_file":
		return "write"
	default:
		return "read"
	}
}
