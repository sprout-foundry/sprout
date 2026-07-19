package agent

import (
	"crypto/md5"
	"fmt"
	"time"
)

// generateSessionID returns a unique session identifier based on the
// current time. Used by NewChangeTracker when the agent has no
// pre-existing session ID, and as input to revision-id generation.
//
// SP-075-extension: extracted from change_tracking.go. No behavior change.
func generateSessionID() string {
	return fmt.Sprintf("agent-%d", time.Now().UnixNano())
}

// generateRevisionID hashes sessionID + instructions + a timestamp to
// produce a stable per-session identifier for history storage. The
// leading "agent-" prefix and 16-char truncation keep the on-disk
// record compact while preserving collision resistance for the
// session-lifetime span.
//
// SP-075-extension: extracted from change_tracking.go. No behavior change.
func generateRevisionID(sessionID, instructions string) string {
	// Create a unique revision ID based on session and instructions
	hash := md5.Sum([]byte(sessionID + "-" + instructions + "-" + fmt.Sprint(time.Now().UnixNano())))
	return fmt.Sprintf("agent-%x", hash)[:16] // Truncate to reasonable length
}

// determineWriteOperation classifies a write as "create" (no prior
// content), "write" (content changed), or "overwrite" (identical
// content). The op drives ChangeTracker.Changes[i].Operation, which
// feeds git-style checkpoint manifests and bulk-rollup heuristics.
//
// SP-075-extension: extracted from change_tracking.go. No behavior change.
func determineWriteOperation(originalContent, newContent string) string {
	if originalContent == "" {
		return "create"
	}
	if originalContent != newContent {
		return "write"
	}
	return "overwrite"
}

// getAgentModel returns the model identifier from the agent's runtime
// config (e.g. "claude-opus-4-5", "gpt-5", "gemini-2.5-pro"). Falls
// back to "unknown" when the tracker has no agent reference, which
// happens for tests that build a bare tracker.
//
// SP-075-extension: extracted from change_tracking.go. No behavior change.
func (ct *ChangeTracker) getAgentModel() string {
	if ct.agent != nil {
		return ct.agent.GetModel()
	}
	return "unknown"
}

// limitString truncates a string to the specified length with ellipsis.
// Used by summary paths to keep LLM context windows bounded.
//
// SP-075-extension: extracted from change_tracking.go. No behavior change.
func limitString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
